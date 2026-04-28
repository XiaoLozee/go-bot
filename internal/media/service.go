package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	neturl "net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/XiaoLozee/go-bot/internal/adapter"
	"github.com/XiaoLozee/go-bot/internal/config"
	"github.com/XiaoLozee/go-bot/internal/domain/event"
	"github.com/XiaoLozee/go-bot/internal/domain/message"
)

const defaultQueueSize = 128

type Service struct {
	logger     *slog.Logger
	cfg        config.MediaConfig
	store      Store
	backend    backend
	httpClient *http.Client
	jobs       chan captureTask
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	enabled    bool
}

type captureTask struct {
	evt    event.Event
	client adapter.ActionClient
}

type sourceInfo struct {
	OriginRef   string
	OriginURL   string
	LocalPath   string
	FileName    string
	ContentType string
}

func NewService(cfg *config.Config, logger *slog.Logger) (*Service, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg == nil {
		return nil, fmt.Errorf("配置为空")
	}

	service := &Service{
		logger:  logger.With("component", "media_store"),
		cfg:     cfg.Storage.Media,
		enabled: cfg.Storage.Media.Enabled,
	}
	if !service.enabled {
		return service, nil
	}

	store, err := openStore(context.Background(), cfg.Storage, service.logger)
	if err != nil {
		return nil, err
	}

	var mediaBackend backend
	switch normalizeMediaBackend(cfg.Storage.Media.Backend) {
	case "r2":
		mediaBackend, err = newR2Backend(context.Background(), cfg.Storage.Media.R2)
	default:
		mediaBackend, err = newLocalBackend(cfg.Storage.Media.Local.Dir)
	}
	if err != nil {
		_ = store.Close()
		return nil, err
	}

	service.store = store
	service.backend = mediaBackend
	service.httpClient = &http.Client{
		Timeout: time.Duration(maxInt(cfg.Storage.Media.DownloadTimeoutSeconds, 20)) * time.Second,
	}
	service.jobs = make(chan captureTask, defaultQueueSize)
	return service, nil
}

func (s *Service) Start(ctx context.Context) {
	if s == nil || !s.enabled || s.jobs == nil || s.cancel != nil {
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.wg.Add(1)
	go s.worker(runCtx)
}

func (s *Service) Close() error {
	if s == nil {
		return nil
	}
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
	s.cancel = nil
	if s.store != nil {
		return s.store.Close()
	}
	return nil
}

func (s *Service) Enqueue(evt event.Event, client adapter.ActionClient) {
	if s == nil || !s.enabled || s.jobs == nil || evt.Kind != "message" {
		return
	}
	if evt.ChatType != "group" && evt.ChatType != "private" {
		return
	}
	if !hasMediaSegments(evt.Segments) {
		return
	}
	select {
	case s.jobs <- captureTask{evt: evt, client: client}:
	default:
		s.logger.Warn("媒体采集队列已满，本次消息跳过", "message_id", evt.MessageID, "connection", evt.ConnectionID)
	}
}

func (s *Service) worker(ctx context.Context) {
	defer s.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case task, ok := <-s.jobs:
			if !ok {
				return
			}
			s.captureEvent(ctx, task)
		}
	}
}

func (s *Service) captureEvent(ctx context.Context, task captureTask) {
	for idx, seg := range task.evt.Segments {
		segmentType := normalizeSegmentType(seg.Type)
		if segmentType == "" {
			continue
		}
		s.captureSegment(ctx, task.evt, idx, seg, task.client, segmentType)
	}
}

func (s *Service) captureSegment(ctx context.Context, evt event.Event, index int, seg message.Segment, client adapter.ActionClient, segmentType string) {
	now := eventTimestampOrNow(evt.Timestamp)
	asset := Asset{
		ID:              buildAssetID(evt, index),
		MessageID:       firstNonEmpty(evt.MessageID, evt.ID),
		ConnectionID:    evt.ConnectionID,
		ChatType:        evt.ChatType,
		GroupID:         evt.GroupID,
		UserID:          evt.UserID,
		SegmentIndex:    index,
		SegmentType:     segmentType,
		StorageBackend:  s.backend.Name(),
		Status:          "pending",
		SegmentDataJSON: marshalSegmentData(seg.Data),
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	source, err := s.resolveSource(ctx, seg, segmentType, client)
	if err != nil {
		asset.Status = "failed"
		asset.Error = err.Error()
		asset.UpdatedAt = time.Now()
		s.persistAsset(ctx, asset)
		return
	}

	asset.OriginRef = source.OriginRef
	asset.OriginURL = source.OriginURL
	asset.FileName = source.FileName

	staged, err := s.stageSource(ctx, source)
	if err != nil {
		asset.Status = "failed"
		asset.Error = err.Error()
		asset.UpdatedAt = time.Now()
		s.persistAsset(ctx, asset)
		return
	}
	defer func() { _ = os.Remove(staged.TempPath) }()

	asset.FileName = firstNonEmpty(asset.FileName, staged.FileName)
	asset.MimeType = staged.MimeType
	asset.SizeBytes = staged.SizeBytes
	asset.SHA256 = staged.SHA256

	stored, err := s.backend.Save(ctx, asset, staged)
	if err != nil {
		asset.Status = "failed"
		asset.Error = err.Error()
		asset.UpdatedAt = time.Now()
		s.persistAsset(ctx, asset)
		return
	}

	asset.StorageKey = stored.Key
	asset.PublicURL = stored.PublicURL
	asset.Status = "stored"
	asset.Error = ""
	asset.UpdatedAt = time.Now()
	s.persistAsset(ctx, asset)
}

func (s *Service) persistAsset(ctx context.Context, asset Asset) {
	if s.store == nil {
		return
	}
	if err := s.store.UpsertAsset(ctx, asset); err != nil {
		s.logger.Warn("写入媒体资源元数据失败", "error", err, "message_id", asset.MessageID, "segment_index", asset.SegmentIndex)
	}
}

func (s *Service) resolveSource(ctx context.Context, seg message.Segment, segmentType string, client adapter.ActionClient) (sourceInfo, error) {
	originRef := firstNonEmpty(segmentString(seg.Data, "url"), segmentString(seg.Data, "file"), segmentString(seg.Data, "path"))
	info := sourceInfo{
		OriginRef: originRef,
		FileName:  firstNonEmpty(segmentString(seg.Data, "name"), fileNameFromPath(originRef)),
	}

	if rawURL := firstNonEmpty(segmentString(seg.Data, "url"), segmentString(seg.Data, "file")); isHTTPURL(rawURL) {
		info.OriginURL = rawURL
		return info, nil
	}

	localPath := firstNonEmpty(segmentString(seg.Data, "path"), resolveFileURL(segmentString(seg.Data, "file")), segmentString(seg.Data, "file"))
	if isLocalPath(localPath) {
		info.LocalPath = localPath
		if info.FileName == "" {
			info.FileName = fileNameFromPath(localPath)
		}
		return info, nil
	}

	if client == nil {
		return sourceInfo{}, fmt.Errorf("媒体段缺少可读取的 url/path，且当前连接不支持媒体解析")
	}

	resolved, err := client.ResolveMedia(ctx, segmentType, originRef)
	if err != nil {
		return sourceInfo{}, err
	}
	if resolved == nil {
		return sourceInfo{}, fmt.Errorf("未获取到媒体解析结果")
	}

	info.OriginURL = strings.TrimSpace(resolved.URL)
	info.LocalPath = resolveFileURL(resolved.File)
	info.FileName = firstNonEmpty(info.FileName, strings.TrimSpace(resolved.FileName), fileNameFromPath(info.LocalPath), fileNameFromPath(info.OriginURL))
	if isHTTPURL(info.OriginURL) {
		return info, nil
	}
	if isLocalPath(info.LocalPath) {
		return info, nil
	}
	return sourceInfo{}, fmt.Errorf("媒体解析结果中没有可读取的本地文件或 URL")
}

func (s *Service) stageSource(ctx context.Context, source sourceInfo) (stagedFile, error) {
	tmpFile, err := os.CreateTemp("", "gobot-media-*")
	if err != nil {
		return stagedFile{}, fmt.Errorf("创建临时媒体文件失败: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		_ = tmpFile.Close()
	}()

	var reader io.ReadCloser
	var contentType string
	var fileName string
	maxBytes := int64(maxInt(s.cfg.MaxSizeMB, 1)) * 1024 * 1024

	switch {
	case source.LocalPath != "":
		file, err := os.Open(source.LocalPath)
		if err != nil {
			return stagedFile{}, fmt.Errorf("打开本地媒体文件失败: %w", err)
		}
		stat, err := file.Stat()
		if err == nil && stat.Size() > maxBytes {
			_ = file.Close()
			return stagedFile{}, fmt.Errorf("媒体文件超过大小限制：%.2fMB > %dMB", float64(stat.Size())/(1024*1024), s.cfg.MaxSizeMB)
		}
		reader = file
		fileName = firstNonEmpty(source.FileName, statName(stat), fileNameFromPath(source.LocalPath))
	case source.OriginURL != "":
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.OriginURL, nil)
		if err != nil {
			return stagedFile{}, fmt.Errorf("创建媒体下载请求失败: %w", err)
		}
		resp, err := s.httpClient.Do(req)
		if err != nil {
			return stagedFile{}, fmt.Errorf("下载媒体文件失败: %w", err)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			_ = resp.Body.Close()
			return stagedFile{}, fmt.Errorf("下载媒体文件失败: HTTP %d", resp.StatusCode)
		}
		if resp.ContentLength > maxBytes && resp.ContentLength > 0 {
			_ = resp.Body.Close()
			return stagedFile{}, fmt.Errorf("媒体文件超过大小限制：%.2fMB > %dMB", float64(resp.ContentLength)/(1024*1024), s.cfg.MaxSizeMB)
		}
		reader = resp.Body
		contentType = strings.TrimSpace(resp.Header.Get("Content-Type"))
		fileName = firstNonEmpty(source.FileName, fileNameFromPath(source.OriginURL))
	default:
		return stagedFile{}, fmt.Errorf("缺少可读取的媒体源")
	}
	defer func() { _ = reader.Close() }()

	sizeBytes, shaValue, sniff, err := copyToFileWithLimit(tmpFile, reader, maxBytes)
	if err != nil {
		_ = os.Remove(tmpPath)
		return stagedFile{}, err
	}
	if contentType == "" {
		contentType = http.DetectContentType(sniff)
	}
	extension := inferFileExtension(fileName, contentType)
	return stagedFile{
		TempPath:  tmpPath,
		FileName:  fileName,
		MimeType:  contentType,
		SizeBytes: sizeBytes,
		SHA256:    shaValue,
		Extension: extension,
	}, nil
}

func copyToFileWithLimit(dst *os.File, src io.Reader, maxBytes int64) (int64, string, []byte, error) {
	hasher := sha256.New()
	buffer := make([]byte, 32*1024)
	sniff := make([]byte, 0, 512)
	var written int64
	for {
		n, readErr := src.Read(buffer)
		if n > 0 {
			chunk := buffer[:n]
			written += int64(n)
			if maxBytes > 0 && written > maxBytes {
				return 0, "", nil, fmt.Errorf("媒体文件超过大小限制：%.2fMB > %.2fMB", float64(written)/(1024*1024), float64(maxBytes)/(1024*1024))
			}
			if len(sniff) < 512 {
				remaining := 512 - len(sniff)
				if remaining > n {
					remaining = n
				}
				sniff = append(sniff, chunk[:remaining]...)
			}
			if _, err := dst.Write(chunk); err != nil {
				return 0, "", nil, fmt.Errorf("写入临时媒体文件失败: %w", err)
			}
			if _, err := hasher.Write(chunk); err != nil {
				return 0, "", nil, fmt.Errorf("计算媒体哈希失败: %w", err)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return 0, "", nil, fmt.Errorf("读取媒体内容失败: %w", readErr)
		}
	}
	if err := dst.Sync(); err != nil {
		return 0, "", nil, fmt.Errorf("刷新临时媒体文件失败: %w", err)
	}
	return written, hex.EncodeToString(hasher.Sum(nil)), sniff, nil
}

func hasMediaSegments(segments []message.Segment) bool {
	for _, seg := range segments {
		if normalizeSegmentType(seg.Type) != "" {
			return true
		}
	}
	return false
}

func normalizeSegmentType(segmentType string) string {
	switch strings.TrimSpace(strings.ToLower(segmentType)) {
	case "image":
		return "image"
	case "record", "audio", "voice":
		return "record"
	case "video":
		return "video"
	case "file", "onlinefile":
		return "file"
	default:
		return ""
	}
}

func normalizeMediaBackend(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "r2", "cloudflare_r2", "cloudflare-r2":
		return "r2"
	default:
		return "local"
	}
}

func segmentString(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	value, ok := data[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}

func marshalSegmentData(data map[string]any) string {
	if len(data) == 0 {
		return "{}"
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return "{}"
	}
	return string(payload)
}

func buildAssetID(evt event.Event, index int) string {
	base := firstNonEmpty(evt.MessageID, evt.ID)
	if strings.TrimSpace(base) == "" {
		base = fmt.Sprintf("evt-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%s:%02d", base, index)
}

func eventTimestampOrNow(value time.Time) time.Time {
	if value.IsZero() {
		return time.Now()
	}
	return value
}

func fileNameFromPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if parsed, err := neturl.Parse(raw); err == nil && parsed.Path != "" {
		return path.Base(parsed.Path)
	}
	return filepath.Base(raw)
}

func resolveFileURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(raw), "file://") {
		if parsed, err := neturl.Parse(raw); err == nil {
			return filepath.FromSlash(parsed.Path)
		}
	}
	return raw
}

func isHTTPURL(raw string) bool {
	parsed, err := neturl.Parse(strings.TrimSpace(raw))
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func isLocalPath(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || isHTTPURL(raw) {
		return false
	}
	if strings.HasPrefix(strings.ToLower(raw), "file://") {
		return true
	}
	return filepath.IsAbs(raw) || strings.HasPrefix(raw, ".") || strings.Contains(raw, "\\") || strings.Contains(raw, "/")
}

func inferFileExtension(fileName, contentType string) string {
	ext := strings.TrimSpace(filepath.Ext(fileName))
	if ext != "" {
		return ext
	}
	if contentType == "" {
		return ""
	}
	if exts, err := mime.ExtensionsByType(contentType); err == nil && len(exts) > 0 {
		return exts[0]
	}
	return ""
}

func statName(info os.FileInfo) string {
	if info == nil {
		return ""
	}
	return info.Name()
}

func maxInt(value int, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}
