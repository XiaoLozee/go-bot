package ai

import (
	"context"
	"encoding/base64"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/XiaoLozee/go-bot/internal/config"
	"github.com/XiaoLozee/go-bot/internal/domain/event"
	"github.com/XiaoLozee/go-bot/internal/domain/message"
)

const visionLocalFileSizeLimit = 12 * 1024 * 1024

func (s *Service) describeEventImages(ctx context.Context, evt event.Event) (string, error) {
	s.mu.RLock()
	cfg := applyGroupPolicyToAIConfig(s.cfg, evt)
	analyzer := s.visionGenerator
	s.mu.RUnlock()

	return s.describeEventImagesWithConfig(ctx, evt, cfg, analyzer)
}

func (s *Service) describeEventImagesWithConfig(ctx context.Context, evt event.Event, cfg config.AIConfig, analyzer visionGenerator) (string, error) {
	if !cfg.Enabled || analyzer == nil {
		return "", nil
	}

	images := s.collectVisionInputs(ctx, evt)
	if len(images) == 0 {
		return "", nil
	}

	provider, ok := effectiveVisionProvider(cfg)
	if !ok {
		return "", nil
	}

	maxTokens := cfg.Reply.MaxOutputTokens / 2
	if maxTokens < 96 {
		maxTokens = 96
	}
	if maxTokens > 512 {
		maxTokens = 512
	}

	summary, err := analyzer.Describe(ctx, buildVisionPrompt(len(images)), images, provider, maxTokens)
	if err != nil {
		s.recordVisionFailure(err)
		return "", err
	}
	summary = strings.Join(strings.Fields(strings.TrimSpace(summary)), " ")
	if summary == "" {
		s.recordVisionFailure(fmt.Errorf("视觉模型返回空结果"))
		return "", nil
	}
	s.recordVisionSuccess(summary)
	return "图片识别：" + summary, nil
}

func (s *Service) collectVisionInputs(ctx context.Context, evt event.Event) []visionImageInput {
	inputs := make([]visionImageInput, 0, 2)
	seen := make(map[string]struct{})
	for index, seg := range evt.Segments {
		if strings.TrimSpace(seg.Type) != "image" {
			continue
		}
		input, err := s.resolveVisionImageInput(ctx, evt.ConnectionID, seg)
		if err != nil {
			s.logger.Warn("解析图片消息失败，已跳过该图片", "error", err, "segment_index", index, "group_id", evt.GroupID, "user_id", evt.UserID)
			continue
		}
		key := strings.TrimSpace(input.URL)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		inputs = append(inputs, input)
		if len(inputs) >= 3 {
			break
		}
	}
	return inputs
}

func (s *Service) resolveVisionImageInput(ctx context.Context, connectionID string, seg message.Segment) (visionImageInput, error) {
	if direct := firstNonEmpty(segmentString(seg.Data, "url"), segmentString(seg.Data, "file")); isRemoteImageRef(direct) {
		return visionImageInput{URL: strings.TrimSpace(direct)}, nil
	}
	if localPath, ok := existingLocalPath(
		segmentString(seg.Data, "path"),
		segmentString(seg.Data, "file"),
		segmentString(seg.Data, "url"),
	); ok {
		dataURL, err := localImageToDataURL(localPath)
		if err != nil {
			return visionImageInput{}, err
		}
		return visionImageInput{URL: dataURL}, nil
	}

	originRef := firstNonEmpty(segmentString(seg.Data, "file"), segmentString(seg.Data, "url"), segmentString(seg.Data, "path"))
	if strings.TrimSpace(originRef) == "" {
		return visionImageInput{}, fmt.Errorf("图片段缺少可解析的媒体引用")
	}

	resolved, err := s.messenger.ResolveMedia(ctx, connectionID, "image", originRef)
	if err != nil {
		return visionImageInput{}, err
	}
	if resolved == nil {
		return visionImageInput{}, fmt.Errorf("未获取到图片解析结果")
	}
	if isRemoteImageRef(resolved.URL) {
		return visionImageInput{URL: strings.TrimSpace(resolved.URL)}, nil
	}
	if localPath, ok := existingLocalPath(resolved.File, originRef); ok {
		dataURL, err := localImageToDataURL(localPath)
		if err != nil {
			return visionImageInput{}, err
		}
		return visionImageInput{URL: dataURL}, nil
	}
	return visionImageInput{}, fmt.Errorf("图片解析结果中没有可用的 URL 或本地文件")
}

func buildVisionPrompt(imageCount int) string {
	if imageCount <= 1 {
		return "请识别这张图片中与聊天回复有关的关键信息，用简体中文输出 1 到 3 句：主体是什么、画面里有什么文字、这张图大概表达了什么情绪或梗。看不清的部分不要猜。"
	}
	return "请按“图片1 / 图片2 / 图片3”分别概括这些图片中与聊天回复有关的关键信息，用简体中文简要说明主体、文字信息、情绪或梗，不要编造看不清的细节。"
}

func isRemoteImageRef(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "data:image/")
}

func existingLocalPath(values ...string) (string, bool) {
	for _, value := range values {
		path := normalizeLocalFileRef(value)
		if path == "" || isRemoteImageRef(path) {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		return path, true
	}
	return "", false
}

func normalizeLocalFileRef(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(trimmed), "file://") {
		parsed, err := url.Parse(trimmed)
		if err == nil {
			path, _ := url.PathUnescape(parsed.Path)
			if parsed.Host != "" && parsed.Host != "localhost" {
				path = `\\` + parsed.Host + filepath.FromSlash(path)
			}
			if len(path) >= 3 && path[0] == '/' && path[2] == ':' {
				path = path[1:]
			}
			if strings.TrimSpace(path) != "" {
				return filepath.Clean(filepath.FromSlash(path))
			}
		}
		trimmed = strings.TrimPrefix(trimmed, "file://")
	}
	return filepath.Clean(trimmed)
}

func localImageToDataURL(path string) (string, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取本地图片失败: %w", err)
	}
	if len(payload) == 0 {
		return "", fmt.Errorf("本地图片为空: %s", path)
	}
	if len(payload) > visionLocalFileSizeLimit {
		return "", fmt.Errorf("本地图片超过大小限制（%d MB）", visionLocalFileSizeLimit/1024/1024)
	}

	mimeType := detectImageMIME(path, payload)
	if !strings.HasPrefix(mimeType, "image/") {
		return "", fmt.Errorf("文件不是可识别的图片: %s", path)
	}

	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(payload), nil
}

func detectImageMIME(path string, payload []byte) string {
	sniffSize := len(payload)
	if sniffSize > 512 {
		sniffSize = 512
	}
	mimeType := http.DetectContentType(payload[:sniffSize])
	if strings.HasPrefix(mimeType, "image/") {
		return mimeType
	}
	if byExt := mime.TypeByExtension(strings.ToLower(filepath.Ext(path))); strings.HasPrefix(byExt, "image/") {
		return byExt
	}
	return mimeType
}

func (s *Service) recordVisionSuccess(summary string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastVisionAt = time.Now()
	s.lastVisionSummary = strings.TrimSpace(summary)
	s.lastVisionError = ""
}

func (s *Service) recordVisionFailure(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastVisionAt = time.Now()
	s.lastVisionError = strings.TrimSpace(err.Error())
}
