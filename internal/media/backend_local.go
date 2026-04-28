package media

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type storedObject struct {
	Key       string
	PublicURL string
}

type stagedFile struct {
	TempPath  string
	FileName  string
	MimeType  string
	SizeBytes int64
	SHA256    string
	Extension string
}

type backend interface {
	Name() string
	Save(ctx context.Context, asset Asset, staged stagedFile) (storedObject, error)
}

type localBackend struct {
	baseDir string
}

func newLocalBackend(baseDir string) (*localBackend, error) {
	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		return nil, fmt.Errorf("本地媒体目录不能为空")
	}
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建本地媒体目录失败: %w", err)
	}
	absBase := baseDir
	if resolved, err := filepath.Abs(baseDir); err == nil {
		absBase = resolved
	}
	return &localBackend{baseDir: absBase}, nil
}

func (b *localBackend) Name() string {
	return "local"
}

func (b *localBackend) Save(_ context.Context, asset Asset, staged stagedFile) (storedObject, error) {
	relKey := buildObjectKey("", asset, staged.Extension, staged.SHA256)
	targetPath := filepath.Join(b.baseDir, filepath.FromSlash(relKey))
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return storedObject{}, fmt.Errorf("创建本地媒体子目录失败: %w", err)
	}
	if err := moveOrCopyFile(staged.TempPath, targetPath); err != nil {
		return storedObject{}, fmt.Errorf("保存本地媒体文件失败: %w", err)
	}
	return storedObject{
		Key:       relKey,
		PublicURL: "file://" + filepath.ToSlash(targetPath),
	}, nil
}

func buildObjectKey(prefix string, asset Asset, ext string, sha string) string {
	datePath := asset.CreatedAt.UTC().Format("2006/01/02")
	if datePath == "" || datePath == "0001/01/01" {
		datePath = "unknown-date"
	}
	messageID := sanitizeKeyPart(asset.MessageID)
	if messageID == "" {
		messageID = "msg"
	}
	shortHash := sha
	if len(shortHash) > 16 {
		shortHash = shortHash[:16]
	}
	if shortHash == "" {
		shortHash = "nohash"
	}
	scopeID := sanitizeKeyPart(firstNonEmpty(asset.GroupID, asset.UserID))
	if scopeID == "" {
		scopeID = "unknown"
	}
	segmentType := sanitizeKeyPart(asset.SegmentType)
	if segmentType == "" {
		segmentType = "file"
	}
	filename := fmt.Sprintf("%s-%02d-%s%s", messageID, asset.SegmentIndex, shortHash, normalizeExt(ext))
	parts := []string{}
	if strings.TrimSpace(prefix) != "" {
		parts = append(parts, strings.Trim(strings.ReplaceAll(prefix, "\\", "/"), "/"))
	}
	parts = append(parts, datePath, segmentType, scopeID, filename)
	return path.Join(parts...)
}

func normalizeExt(ext string) string {
	ext = strings.TrimSpace(ext)
	if ext == "" {
		return ""
	}
	if !strings.HasPrefix(ext, ".") {
		return "." + ext
	}
	return ext
}

func sanitizeKeyPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-', r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteRune('_')
		}
	}
	return strings.Trim(builder.String(), "_")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func moveOrCopyFile(srcPath, dstPath string) error {
	if err := os.Rename(srcPath, dstPath); err == nil {
		return nil
	}

	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()

	dst, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer func() { _ = dst.Close() }()

	if _, err := io.Copy(dst, src); err != nil {
		return err
	}
	if err := dst.Sync(); err != nil {
		return err
	}
	return os.Remove(srcPath)
}
