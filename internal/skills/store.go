package skills

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	packageFormatZIP   = "zip"
	packageFormatTarGZ = "tar.gz"

	maxRemoteArchiveBytes = 64 << 20
)

var (
	errArchiveTooLarge = errors.New("技能包过大，当前仅支持 64MB 以内的压缩包")
	errStopSkillScan   = errors.New("stop skill scan")
)

type Store struct {
	mu           sync.RWMutex
	rootDir      string
	itemsDir     string
	backupDir    string
	registryPath string
	client       *http.Client
	items        map[string]InstalledSkill
}

type registryState struct {
	Skills []InstalledSkill `json:"skills"`
}

type remoteSource struct {
	SourceType  string
	SourceLabel string
	Provider    string
	SourceURL   string
	Candidates  []remoteCandidate
}

type remoteCandidate struct {
	URL      string
	FileName string
}

type installSource struct {
	SourceType  string
	SourceLabel string
	SourceURL   string
	Provider    string
}

func NewStore(rootDir string) (*Store, error) {
	rootDir = strings.TrimSpace(rootDir)
	if rootDir == "" {
		return nil, fmt.Errorf("技能目录不能为空")
	}
	if abs, err := filepath.Abs(rootDir); err == nil {
		rootDir = abs
	}
	store := &Store{
		rootDir:      rootDir,
		itemsDir:     filepath.Join(rootDir, "items"),
		backupDir:    filepath.Join(rootDir, ".bak"),
		registryPath: filepath.Join(rootDir, "registry.json"),
		client: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		items: make(map[string]InstalledSkill),
	}
	if err := os.MkdirAll(store.itemsDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建技能目录失败: %w", err)
	}
	if err := os.MkdirAll(store.backupDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建技能备份目录失败: %w", err)
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) List() []InstalledSkill {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]InstalledSkill, 0, len(s.items))
	for _, item := range s.items {
		out = append(out, cloneInstalledSkill(item))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Enabled != out[j].Enabled {
			return out[i].Enabled
		}
		if !out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func (s *Store) Get(id string) (SkillDetail, error) {
	s.mu.RLock()
	item, ok := s.items[strings.TrimSpace(id)]
	s.mu.RUnlock()
	if !ok {
		return SkillDetail{}, fmt.Errorf("技能不存在: %s", id)
	}
	content, err := s.readSkillContent(item)
	if err != nil {
		return SkillDetail{}, err
	}
	item.ContentLength = len(content)
	return SkillDetail{InstalledSkill: item, Content: content}, nil
}

func (s *Store) EnabledPromptSkills() ([]PromptSkill, error) {
	items := s.List()
	out := make([]PromptSkill, 0, len(items))
	for _, item := range items {
		if !item.Enabled {
			continue
		}
		content, err := s.readSkillContent(item)
		if err != nil {
			return nil, err
		}
		content = strings.TrimSpace(content)
		if content == "" {
			continue
		}
		out = append(out, PromptSkill{
			ID:          item.ID,
			Name:        item.Name,
			Description: item.Description,
			Content:     content,
		})
	}
	return out, nil
}

func (s *Store) InstallArchive(_ context.Context, fileName string, payload []byte, overwrite bool) (InstallResult, error) {
	return s.installArchivePayload(fileName, payload, overwrite, installSource{
		SourceType:  "local_upload",
		SourceLabel: "本地上传",
		Provider:    "本地上传",
	})
}

func (s *Store) InstallFromURL(ctx context.Context, sourceURL string, overwrite bool) (InstallResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	resolved, err := s.resolveRemoteSource(ctx, sourceURL, 0)
	if err != nil {
		return InstallResult{}, err
	}
	var lastErr error
	for _, candidate := range resolved.Candidates {
		payload, err := s.downloadRemotePayload(ctx, candidate.URL, maxRemoteArchiveBytes)
		if err != nil {
			lastErr = err
			continue
		}
		result, err := s.installArchivePayload(candidate.FileName, payload, overwrite, installSource{
			SourceType:  resolved.SourceType,
			SourceLabel: resolved.SourceLabel,
			SourceURL:   resolved.SourceURL,
			Provider:    resolved.Provider,
		})
		if err == nil {
			return result, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return InstallResult{}, lastErr
	}
	return InstallResult{}, fmt.Errorf("未找到可下载的技能压缩包")
}

func (s *Store) SetEnabled(id string, enabled bool) (InstalledSkill, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, ok := s.items[strings.TrimSpace(id)]
	if !ok {
		return InstalledSkill{}, fmt.Errorf("技能不存在: %s", id)
	}
	item.Enabled = enabled
	item.UpdatedAt = time.Now()
	s.items[item.ID] = item
	if err := s.saveLocked(); err != nil {
		return InstalledSkill{}, err
	}
	return cloneInstalledSkill(item), nil
}

func (s *Store) Uninstall(id string) (InstalledSkill, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, ok := s.items[strings.TrimSpace(id)]
	if !ok {
		return InstalledSkill{}, fmt.Errorf("技能不存在: %s", id)
	}
	if err := ensureWithinRoot(s.rootDir, item.RootDir); err != nil {
		return InstalledSkill{}, err
	}
	if err := os.RemoveAll(item.RootDir); err != nil {
		return InstalledSkill{}, fmt.Errorf("删除技能目录失败: %w", err)
	}
	delete(s.items, item.ID)
	if err := s.saveLocked(); err != nil {
		return InstalledSkill{}, err
	}
	return cloneInstalledSkill(item), nil
}

func (s *Store) load() error {
	payload, err := os.ReadFile(s.registryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("读取技能注册表失败: %w", err)
	}
	if len(bytes.TrimSpace(payload)) == 0 {
		return nil
	}
	var state registryState
	if err := json.Unmarshal(payload, &state); err != nil {
		return fmt.Errorf("解析技能注册表失败: %w", err)
	}
	for _, item := range state.Skills {
		item.ID = strings.TrimSpace(item.ID)
		if item.ID == "" {
			continue
		}
		if item.RootDir == "" {
			item.RootDir = filepath.Join(s.itemsDir, item.ID)
		}
		s.items[item.ID] = item
	}
	return nil
}

func (s *Store) saveLocked() error {
	state := registryState{Skills: make([]InstalledSkill, 0, len(s.items))}
	for _, item := range s.items {
		state.Skills = append(state.Skills, cloneInstalledSkill(item))
	}
	sort.Slice(state.Skills, func(i, j int) bool { return state.Skills[i].ID < state.Skills[j].ID })
	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化技能注册表失败: %w", err)
	}
	payload = append(payload, '\n')
	if err := os.MkdirAll(filepath.Dir(s.registryPath), 0o755); err != nil {
		return fmt.Errorf("创建技能注册表目录失败: %w", err)
	}
	tmpPath := s.registryPath + ".tmp"
	if err := os.WriteFile(tmpPath, payload, 0o644); err != nil {
		return fmt.Errorf("写入技能注册表临时文件失败: %w", err)
	}
	if err := os.Rename(tmpPath, s.registryPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("替换技能注册表失败: %w", err)
	}
	return nil
}

func (s *Store) installArchivePayload(fileName string, payload []byte, overwrite bool, source installSource) (InstallResult, error) {
	if len(payload) == 0 {
		return InstallResult{}, fmt.Errorf("上传文件为空")
	}
	format, err := detectPackageFormat(fileName)
	if err != nil {
		return InstallResult{}, err
	}

	stageRoot, err := os.MkdirTemp(filepath.Dir(s.rootDir), ".gobot-skill-stage-*")
	if err != nil {
		return InstallResult{}, fmt.Errorf("创建技能暂存目录失败: %w", err)
	}
	defer func() { _ = os.RemoveAll(stageRoot) }()

	if err := extractPackage(stageRoot, payload, format); err != nil {
		return InstallResult{}, err
	}
	match, err := locateSkillEntry(stageRoot)
	if err != nil {
		return InstallResult{}, err
	}
	contentBytes, err := os.ReadFile(match.skillPath)
	if err != nil {
		return InstallResult{}, fmt.Errorf("读取 SKILL.md 失败: %w", err)
	}
	content := strings.TrimSpace(string(contentBytes))
	if content == "" {
		return InstallResult{}, fmt.Errorf("SKILL.md 为空")
	}
	meta := parseSkillMetadata(content)
	if meta.Name == "" {
		meta.Name = normalizeDisplayName(filepath.Base(match.skillRoot))
	}

	baseID := preferredSkillIDBase(fileName, source.SourceURL, meta.Name, filepath.Base(match.skillRoot))
	candidateID := buildSkillID(baseID, content)
	entryPath, err := filepath.Rel(match.skillRoot, match.skillPath)
	if err != nil {
		return InstallResult{}, fmt.Errorf("计算技能入口路径失败: %w", err)
	}
	entryPath = filepath.ToSlash(entryPath)

	now := time.Now()
	item := InstalledSkill{
		ID:                 candidateID,
		Name:               meta.Name,
		Description:        meta.Description,
		SourceType:         firstNonEmpty(source.SourceType, "local_upload"),
		SourceLabel:        firstNonEmpty(source.SourceLabel, sourceLabelForType(source.SourceType), "本地上传"),
		SourceURL:          strings.TrimSpace(source.SourceURL),
		Provider:           firstNonEmpty(source.Provider, providerLabelForType(source.SourceType), "本地上传"),
		Enabled:            true,
		InstalledAt:        now,
		UpdatedAt:          now,
		EntryPath:          entryPath,
		RootDir:            filepath.Join(s.itemsDir, candidateID),
		Format:             format,
		InstructionPreview: previewText(content, 280),
		ContentLength:      len(content),
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	existing, exists := s.items[item.ID]
	if exists {
		item.InstalledAt = existing.InstalledAt
		if !overwrite {
			return InstallResult{}, fmt.Errorf("技能已存在: %s；如需更新请使用覆盖安装", item.ID)
		}
	}
	result, err := s.persistInstalledSkillLocked(item, match.skillRoot)
	if err != nil {
		return InstallResult{}, err
	}
	if exists {
		result.Replaced = true
	}
	return result, nil
}

func (s *Store) persistInstalledSkillLocked(item InstalledSkill, sourceRoot string) (InstallResult, error) {
	if err := os.MkdirAll(s.itemsDir, 0o755); err != nil {
		return InstallResult{}, fmt.Errorf("创建技能存储目录失败: %w", err)
	}
	targetDir := filepath.Join(s.itemsDir, item.ID)
	if err := ensureWithinRoot(s.rootDir, targetDir); err != nil {
		return InstallResult{}, err
	}
	exists, err := pathExists(targetDir)
	if err != nil {
		return InstallResult{}, fmt.Errorf("检查技能目录失败: %w", err)
	}
	backupPath := ""
	previous, hadPrevious := s.items[item.ID]
	if exists {
		backupPath = filepath.Join(s.backupDir, item.ID+"-"+time.Now().Format("20060102-150405.000"))
		if err := os.MkdirAll(filepath.Dir(backupPath), 0o755); err != nil {
			return InstallResult{}, fmt.Errorf("创建技能备份目录失败: %w", err)
		}
		if err := moveDir(targetDir, backupPath); err != nil {
			return InstallResult{}, fmt.Errorf("备份旧技能目录失败: %w", err)
		}
	}
	if err := moveDir(sourceRoot, targetDir); err != nil {
		if backupPath != "" {
			_ = restoreDir(targetDir, backupPath)
		}
		return InstallResult{}, fmt.Errorf("移动技能目录失败: %w", err)
	}
	item.RootDir = targetDir
	content, err := s.readSkillContent(item)
	if err != nil {
		if backupPath != "" {
			_ = restoreDir(targetDir, backupPath)
		}
		return InstallResult{}, err
	}
	item.ContentLength = len(content)
	item.InstructionPreview = previewText(content, 280)
	s.items[item.ID] = item
	if err := s.saveLocked(); err != nil {
		if hadPrevious {
			s.items[item.ID] = previous
		} else {
			delete(s.items, item.ID)
		}
		if backupPath != "" {
			_ = restoreDir(targetDir, backupPath)
		}
		return InstallResult{}, err
	}
	return InstallResult{
		Skill: SkillDetail{
			InstalledSkill: cloneInstalledSkill(item),
			Content:        content,
		},
		InstalledTo: targetDir,
		BackupPath:  backupPath,
		Replaced:    backupPath != "",
	}, nil
}

func (s *Store) resolveRemoteSource(ctx context.Context, sourceURL string, _ int) (remoteSource, error) {
	parsed, err := normalizeRemoteURL(sourceURL)
	if err != nil {
		return remoteSource{}, err
	}
	if err := validateRemoteHost(ctx, parsed); err != nil {
		return remoteSource{}, err
	}
	if direct := directArchiveCandidate(parsed.String()); direct != nil {
		return remoteSource{
			SourceType:  sourceTypeForHost(parsed.Hostname(), true),
			SourceLabel: sourceLabelForHost(parsed.Hostname(), true),
			Provider:    providerLabelForHost(parsed.Hostname(), true),
			SourceURL:   parsed.String(),
			Candidates:  []remoteCandidate{*direct},
		}, nil
	}
	if candidates := repositoryArchiveCandidates(parsed); len(candidates) > 0 {
		return remoteSource{
			SourceType:  sourceTypeForHost(parsed.Hostname(), false),
			SourceLabel: sourceLabelForHost(parsed.Hostname(), false),
			Provider:    providerLabelForHost(parsed.Hostname(), false),
			SourceURL:   parsed.String(),
			Candidates:  candidates,
		}, nil
	}
	return remoteSource{}, fmt.Errorf("当前仅支持 GitHub 仓库页、Gitee 仓库页或直链压缩包链接")
}

func (s *Store) downloadRemotePayload(ctx context.Context, sourceURL string, limit int64) ([]byte, error) {
	payload, _, _, err := s.fetchRemoteBytes(ctx, sourceURL, limit)
	return payload, err
}

func (s *Store) fetchRemoteBytes(ctx context.Context, sourceURL string, limit int64) ([]byte, string, http.Header, error) {
	currentURL := sourceURL
	for redirectHop := 0; redirectHop <= 5; redirectHop++ {
		parsed, err := normalizeRemoteURL(currentURL)
		if err != nil {
			return nil, "", nil, err
		}
		if err := validateRemoteHost(ctx, parsed); err != nil {
			return nil, "", nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
		if err != nil {
			return nil, "", nil, fmt.Errorf("创建远程请求失败: %w", err)
		}
		resp, err := s.client.Do(req)
		if err != nil {
			return nil, "", nil, fmt.Errorf("获取远程技能失败: %w", err)
		}
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			location := strings.TrimSpace(resp.Header.Get("Location"))
			_ = resp.Body.Close()
			if location == "" {
				return nil, "", nil, fmt.Errorf("远程技能跳转缺少 Location")
			}
			nextURL, err := parsed.Parse(location)
			if err != nil {
				return nil, "", nil, fmt.Errorf("解析远程技能跳转地址失败: %w", err)
			}
			currentURL = nextURL.String()
			continue
		}
		if resp.StatusCode != http.StatusOK {
			payload, _ := readLimited(resp.Body, 16<<10)
			_ = resp.Body.Close()
			return nil, "", nil, fmt.Errorf("获取远程技能失败: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(payload)))
		}
		payload, err := readLimited(resp.Body, limit)
		_ = resp.Body.Close()
		if err != nil {
			return nil, "", nil, err
		}
		return payload, parsed.String(), resp.Header, nil
	}
	return nil, "", nil, fmt.Errorf("远程技能跳转过多")
}

func detectPackageFormat(fileName string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(fileName))
	switch {
	case strings.HasSuffix(name, ".zip"):
		return packageFormatZIP, nil
	case strings.HasSuffix(name, ".tar.gz"), strings.HasSuffix(name, ".tgz"):
		return packageFormatTarGZ, nil
	default:
		return "", fmt.Errorf("当前仅支持 .zip / .tar.gz / .tgz 技能包")
	}
}

func extractPackage(targetRoot string, payload []byte, format string) error {
	switch format {
	case packageFormatZIP:
		return unzipPackage(targetRoot, payload)
	case packageFormatTarGZ:
		return untarGzipPackage(targetRoot, payload)
	default:
		return fmt.Errorf("不支持的技能包格式: %s", format)
	}
}

type locatedSkill struct {
	skillRoot string
	skillPath string
}

func locateSkillEntry(root string) (locatedSkill, error) {
	matches := make([]string, 0, 2)
	err := filepath.Walk(root, func(current string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info == nil || info.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Base(current), "SKILL.md") {
			return nil
		}
		matches = append(matches, current)
		if len(matches) > 1 {
			return errStopSkillScan
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStopSkillScan) {
		return locatedSkill{}, fmt.Errorf("扫描技能包失败: %w", err)
	}
	switch len(matches) {
	case 0:
		return locatedSkill{}, fmt.Errorf("压缩包中未发现 SKILL.md")
	case 1:
		return locatedSkill{skillRoot: filepath.Dir(matches[0]), skillPath: matches[0]}, nil
	default:
		return locatedSkill{}, fmt.Errorf("一个技能包只能包含一个 SKILL.md，当前发现 %d 个", len(matches))
	}
}

func parseSkillMetadata(content string) struct{ Name, Description string } {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	name := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			name = strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
			break
		}
	}
	paragraph := firstParagraphAfterHeading(lines)
	return struct{ Name, Description string }{
		Name:        name,
		Description: paragraph,
	}
}

func firstParagraphAfterHeading(lines []string) string {
	started := false
	parts := make([]string, 0, 4)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !started {
			if strings.HasPrefix(trimmed, "# ") {
				started = true
			}
			continue
		}
		if trimmed == "" {
			if len(parts) > 0 {
				break
			}
			continue
		}
		if strings.HasPrefix(trimmed, "#") && len(parts) == 0 {
			continue
		}
		parts = append(parts, trimmed)
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func previewText(content string, limit int) string {
	if limit <= 0 {
		limit = 280
	}
	normalized := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(content, "\r\n", "\n"), "\t", "  "))
	if len(normalized) <= limit {
		return normalized
	}
	return strings.TrimSpace(normalized[:limit]) + "…"
}

func directArchiveCandidate(rawURL string) *remoteCandidate {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil
	}
	name := archiveFileName(parsed.Path)
	if name == "" {
		return nil
	}
	return &remoteCandidate{URL: parsed.String(), FileName: name}
}

func repositoryArchiveCandidates(parsed *url.URL) []remoteCandidate {
	host := strings.ToLower(parsed.Hostname())
	segments := splitPathSegments(parsed.Path)
	if len(segments) < 2 {
		return nil
	}
	owner, repo := segments[0], segments[1]
	branch := ""
	if len(segments) >= 4 && strings.EqualFold(segments[2], "tree") {
		branch = segments[3]
	}
	if branch == "" {
		branch = "main"
	}
	switch host {
	case "github.com", "www.github.com":
		branches := uniqueBranches(branch, "master")
		out := make([]remoteCandidate, 0, len(branches))
		for _, item := range branches {
			out = append(out, remoteCandidate{
				URL:      fmt.Sprintf("https://codeload.github.com/%s/%s/zip/refs/heads/%s", owner, repo, item),
				FileName: fmt.Sprintf("%s-%s.zip", repo, item),
			})
		}
		return out
	case "gitee.com", "www.gitee.com":
		branches := uniqueBranches(branch, "master")
		out := make([]remoteCandidate, 0, len(branches))
		for _, item := range branches {
			out = append(out, remoteCandidate{
				URL:      fmt.Sprintf("https://gitee.com/%s/%s/repository/archive/%s.zip", owner, repo, item),
				FileName: fmt.Sprintf("%s-%s.zip", repo, item),
			})
		}
		return out
	default:
		return nil
	}
}

func normalizeRemoteURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("技能来源 URL 非法: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("当前仅支持 http / https 技能来源")
	}
	if strings.TrimSpace(parsed.Hostname()) == "" {
		return nil, fmt.Errorf("技能来源缺少主机名")
	}
	port := parsed.Port()
	if port != "" && port != "80" && port != "443" {
		return nil, fmt.Errorf("当前仅允许 80 / 443 端口的远程技能来源")
	}
	parsed.Fragment = ""
	return parsed, nil
}

func validateRemoteHost(ctx context.Context, parsed *url.URL) error {
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "localhost" || strings.HasSuffix(host, ".local") {
		return fmt.Errorf("禁止访问本地技能来源")
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		if isBlockedAddr(addr) {
			return fmt.Errorf("技能来源地址不允许访问: %s", host)
		}
		return nil
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("解析技能来源地址失败: %w", err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("未解析到技能来源地址")
	}
	for _, item := range ips {
		addr, ok := netip.AddrFromSlice(item.IP)
		if !ok {
			continue
		}
		if isBlockedAddr(addr) {
			return fmt.Errorf("技能来源地址不允许访问: %s", addr.String())
		}
	}
	return nil
}

func isBlockedAddr(addr netip.Addr) bool {
	if !addr.IsValid() {
		return true
	}
	if addr.IsLoopback() || addr.IsMulticast() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsPrivate() || addr.IsUnspecified() {
		return true
	}
	if addr.Is4() {
		if addr.Is4In6() {
			return isBlockedAddr(addr.Unmap())
		}
		if netip.MustParsePrefix("100.64.0.0/10").Contains(addr) {
			return true
		}
	}
	return false
}

func readLimited(reader io.Reader, limit int64) ([]byte, error) {
	limited := &io.LimitedReader{R: reader, N: limit + 1}
	payload, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > limit {
		return nil, errArchiveTooLarge
	}
	return payload, nil
}

func unzipPackage(targetRoot string, payload []byte) error {
	reader, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		return fmt.Errorf("解析 zip 技能包失败: %w", err)
	}
	for _, file := range reader.File {
		entryName, err := normalizeArchiveEntryName(file.Name)
		if err != nil {
			return err
		}
		if entryName == "" {
			continue
		}
		targetPath, err := archiveTargetPath(targetRoot, entryName)
		if err != nil {
			return err
		}
		info := file.FileInfo()
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("当前不支持带符号链接的技能包: %s", file.Name)
		}
		if info.IsDir() {
			if err := ensureArchiveDir(targetPath, info.Mode().Perm()); err != nil {
				return err
			}
			continue
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("技能包包含不支持的文件类型: %s", file.Name)
		}
		src, err := file.Open()
		if err != nil {
			return fmt.Errorf("打开技能包文件失败 %s: %w", file.Name, err)
		}
		if err := writeArchiveFile(targetPath, src, info.Mode().Perm(), file.Name); err != nil {
			_ = src.Close()
			return err
		}
		if err := src.Close(); err != nil {
			return fmt.Errorf("关闭技能包文件失败 %s: %w", file.Name, err)
		}
	}
	return nil
}

func untarGzipPackage(targetRoot string, payload []byte) error {
	gzipReader, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("解析 tar.gz 技能包失败: %w", err)
	}
	defer func() { _ = gzipReader.Close() }()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("读取 tar.gz 技能包失败: %w", err)
		}
		entryName, err := normalizeArchiveEntryName(header.Name)
		if err != nil {
			return err
		}
		if entryName == "" {
			continue
		}
		targetPath, err := archiveTargetPath(targetRoot, entryName)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := ensureArchiveDir(targetPath, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := writeArchiveFile(targetPath, tarReader, os.FileMode(header.Mode), header.Name); err != nil {
				return err
			}
		default:
			return fmt.Errorf("技能包包含不支持的文件类型: %s", header.Name)
		}
	}
}

func normalizeArchiveEntryName(name string) (string, error) {
	entryName := path.Clean(strings.ReplaceAll(name, "\\", "/"))
	switch entryName {
	case ".", "/", "":
		return "", nil
	}
	if strings.HasPrefix(entryName, "../") || entryName == ".." || path.IsAbs(entryName) {
		return "", fmt.Errorf("技能包包含非法路径: %s", name)
	}
	return entryName, nil
}

func archiveTargetPath(root, entryName string) (string, error) {
	targetPath := filepath.Join(root, filepath.FromSlash(entryName))
	if !pathWithinRoot(root, targetPath) {
		return "", fmt.Errorf("技能包包含越界路径: %s", entryName)
	}
	return targetPath, nil
}

func ensureArchiveDir(targetPath string, mode os.FileMode) error {
	if mode == 0 {
		mode = 0o755
	}
	if err := os.MkdirAll(targetPath, mode); err != nil {
		return fmt.Errorf("创建技能目录失败 %s: %w", targetPath, err)
	}
	return nil
}

func writeArchiveFile(targetPath string, src io.Reader, mode os.FileMode, displayName string) error {
	if mode == 0 {
		mode = 0o644
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Errorf("创建技能文件目录失败 %s: %w", targetPath, err)
	}
	dst, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("创建技能文件失败 %s: %w", targetPath, err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		return fmt.Errorf("写入技能文件失败 %s: %w", targetPath, err)
	}
	if err := dst.Close(); err != nil {
		return fmt.Errorf("关闭技能文件失败 %s: %w", displayName, err)
	}
	return nil
}

func (s *Store) readSkillContent(item InstalledSkill) (string, error) {
	contentPath := filepath.Join(item.RootDir, filepath.FromSlash(item.EntryPath))
	if err := ensureWithinRoot(s.rootDir, contentPath); err != nil {
		return "", err
	}
	payload, err := os.ReadFile(contentPath)
	if err != nil {
		return "", fmt.Errorf("读取技能说明失败: %w", err)
	}
	return strings.TrimSpace(string(payload)), nil
}

func pathExists(target string) (bool, error) {
	_, err := os.Stat(target)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func moveDir(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else if !isCrossDeviceError(err) {
		return err
	}
	if err := copyDir(src, dst); err != nil {
		return err
	}
	return os.RemoveAll(src)
}

func restoreDir(targetDir, backupPath string) error {
	_ = os.RemoveAll(targetDir)
	if strings.TrimSpace(backupPath) == "" {
		return nil
	}
	exists, err := pathExists(backupPath)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	return moveDir(backupPath, targetDir)
}

func isCrossDeviceError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "cross-device") || strings.Contains(text, "not same device")
}

func copyDir(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("源路径不是目录: %s", src)
	}
	if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
		return err
	}
	return filepath.Walk(src, func(current string, fileInfo os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, current)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		targetPath := filepath.Join(dst, rel)
		if fileInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("目录复制不支持符号链接: %s", current)
		}
		if fileInfo.IsDir() {
			return os.MkdirAll(targetPath, fileInfo.Mode().Perm())
		}
		return copyFile(current, targetPath, fileInfo.Mode().Perm())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func ensureWithinRoot(root, target string) error {
	resolvedRoot := root
	resolvedTarget := target
	if abs, err := filepath.Abs(root); err == nil {
		resolvedRoot = abs
	}
	if abs, err := filepath.Abs(target); err == nil {
		resolvedTarget = abs
	}
	if !pathWithinRoot(resolvedRoot, resolvedTarget) {
		return fmt.Errorf("技能目录超出允许范围: %s", resolvedTarget)
	}
	return nil
}

func pathWithinRoot(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}

func cloneInstalledSkill(item InstalledSkill) InstalledSkill {
	cloned := item
	if cloned.RootDir == "" && cloned.ID != "" {
		cloned.RootDir = item.RootDir
	}
	return cloned
}

func archiveFileName(pathValue string) string {
	base := path.Base(strings.TrimSpace(pathValue))
	lower := strings.ToLower(base)
	switch {
	case strings.HasSuffix(lower, ".tar.gz"):
		return base
	case strings.HasSuffix(lower, ".tgz"):
		return base
	case strings.HasSuffix(lower, ".zip"):
		return base
	default:
		return ""
	}
}

func splitPathSegments(pathValue string) []string {
	parts := strings.Split(strings.Trim(pathValue, "/"), "/")
	out := make([]string, 0, len(parts))
	for _, item := range parts {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func uniqueBranches(branch string, fallback string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, 2)
	for _, item := range []string{strings.TrimSpace(branch), strings.TrimSpace(fallback)} {
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func preferredSkillIDBase(fileName, sourceURL, name, rootBase string) string {
	candidates := []string{
		trimArchiveSuffix(filepath.Base(fileName)),
		repositoryBaseName(sourceURL),
		rootBase,
		name,
	}
	for _, item := range candidates {
		if slug := slugify(item); slug != "" {
			return slug
		}
	}
	return "skill"
}

func buildSkillID(base string, content string) string {
	base = slugify(base)
	if base == "" {
		hash := sha1.Sum([]byte(strings.TrimSpace(content)))
		return "skill-" + hex.EncodeToString(hash[:])[:8]
	}
	return base
}

func repositoryBaseName(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	segments := splitPathSegments(parsed.Path)
	if len(segments) >= 2 {
		return segments[1]
	}
	return ""
}

func trimArchiveSuffix(fileName string) string {
	lower := strings.ToLower(fileName)
	switch {
	case strings.HasSuffix(lower, ".tar.gz"):
		return fileName[:len(fileName)-len(".tar.gz")]
	case strings.HasSuffix(lower, ".tgz"):
		return fileName[:len(fileName)-len(".tgz")]
	case strings.HasSuffix(lower, ".zip"):
		return fileName[:len(fileName)-len(".zip")]
	default:
		return fileName
	}
}

func normalizeDisplayName(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(value, ".zip"), ".tgz"), ".tar.gz")
	value = strings.ReplaceAll(value, "-", " ")
	value = strings.ReplaceAll(value, "_", " ")
	return strings.TrimSpace(value)
}

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func sourceTypeForHost(host string, direct bool) string {
	host = strings.ToLower(strings.TrimSpace(host))
	switch host {
	case "github.com", "www.github.com", "codeload.github.com":
		return "github"
	case "gitee.com", "www.gitee.com":
		return "gitee"
	default:
		if direct {
			return "direct_url"
		}
		return "url"
	}
}

func sourceLabelForHost(host string, direct bool) string {
	return sourceLabelForType(sourceTypeForHost(host, direct))
}

func providerLabelForHost(host string, direct bool) string {
	return providerLabelForType(sourceTypeForHost(host, direct))
}

func sourceLabelForType(sourceType string) string {
	switch strings.ToLower(strings.TrimSpace(sourceType)) {
	case "local_upload":
		return "本地上传"
	case "github":
		return "GitHub"
	case "gitee":
		return "Gitee"
	case "direct_url":
		return "直链压缩包"
	default:
		return "远程导入"
	}
}

func providerLabelForType(sourceType string) string {
	return sourceLabelForType(sourceType)
}
