package api

import (
	"bufio"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/XiaoLozee/go-bot/internal/runtime"
)

const (
	defaultAuditLogLimit = 50
	maxAuditLogLimit     = 200

	defaultAuditSystemLogLimit = 200
	maxAuditSystemLogLimit     = 1000
)

var errAuditSystemLogNotFound = errors.New("system log file not found")

type auditQuery struct {
	Category string
	Action   string
	Result   string
	Target   string
	Query    string
}

func parseAuditLogLimit(raw string) int {
	limit, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || limit <= 0 {
		return defaultAuditLogLimit
	}
	if limit > maxAuditLogLimit {
		return maxAuditLogLimit
	}
	return limit
}

func parseAuditSystemLogLimit(raw string) int {
	limit, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || limit <= 0 {
		return defaultAuditSystemLogLimit
	}
	if limit > maxAuditSystemLogLimit {
		return maxAuditSystemLogLimit
	}
	return limit
}

func parseAuditQuery(r *http.Request) auditQuery {
	if r == nil {
		return auditQuery{}
	}
	query := r.URL.Query()
	return auditQuery{
		Category: strings.ToLower(strings.TrimSpace(query.Get("category"))),
		Action:   strings.ToLower(strings.TrimSpace(query.Get("action"))),
		Result:   strings.ToLower(strings.TrimSpace(query.Get("result"))),
		Target:   strings.ToLower(strings.TrimSpace(query.Get("target"))),
		Query:    strings.ToLower(strings.TrimSpace(query.Get("q"))),
	}
}

func (q auditQuery) HasFilter() bool {
	return q.Category != "" || q.Action != "" || q.Result != "" || q.Target != "" || q.Query != ""
}

func applyAuditQueryFilters(items []runtime.AuditLogEntry, query auditQuery, limit int) []runtime.AuditLogEntry {
	if len(items) == 0 {
		return nil
	}

	filtered := make([]runtime.AuditLogEntry, 0, len(items))
	for _, item := range items {
		if query.Category != "" && strings.ToLower(strings.TrimSpace(item.Category)) != query.Category {
			continue
		}
		if query.Action != "" && strings.ToLower(strings.TrimSpace(item.Action)) != query.Action {
			continue
		}
		if query.Result != "" && strings.ToLower(strings.TrimSpace(item.Result)) != query.Result {
			continue
		}
		if query.Target != "" && !strings.Contains(strings.ToLower(strings.TrimSpace(item.Target)), query.Target) {
			continue
		}
		if query.Query != "" {
			searchText := strings.ToLower(strings.Join([]string{
				item.Category,
				item.Action,
				item.Target,
				item.Result,
				item.Summary,
				item.Detail,
				item.Username,
				item.RemoteAddr,
				item.Method,
				item.Path,
			}, "\n"))
			if !strings.Contains(searchText, query.Query) {
				continue
			}
		}
		filtered = append(filtered, item)
		if limit > 0 && len(filtered) >= limit {
			break
		}
	}
	return filtered
}

func resolveAuditSystemLogDir(provider auditRouteProvider) string {
	const fallback = "./data/logs"
	if provider == nil {
		return fallback
	}
	view := provider.ConfigView()
	storage, _ := view["storage"].(map[string]any)
	logs, _ := storage["logs"].(map[string]any)
	dir, _ := logs["dir"].(string)
	if strings.TrimSpace(dir) == "" {
		return fallback
	}
	return strings.TrimSpace(dir)
}

func findAuditSystemLogFile(logDir string) (string, error) {
	dir := filepath.Clean(strings.TrimSpace(logDir))
	if dir == "." || dir == "" {
		dir = filepath.Clean("./data/logs")
	}

	preferred := filepath.Join(dir, "app.log")
	if info, err := os.Stat(preferred); err == nil && !info.IsDir() {
		return preferred, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", errAuditSystemLogNotFound
		}
		return "", err
	}

	var selected string
	var selectedAt time.Time
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".log") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if selected == "" || info.ModTime().After(selectedAt) {
			selected = filepath.Join(dir, entry.Name())
			selectedAt = info.ModTime()
		}
	}
	if selected == "" {
		return "", errAuditSystemLogNotFound
	}
	return selected, nil
}

func readAuditSystemLogTail(path string, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	ring := make([]string, limit)
	count := 0
	for scanner.Scan() {
		ring[count%limit] = scanner.Text()
		count++
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	size := count
	if size > limit {
		size = limit
	}
	start := 0
	if count > limit {
		start = count % limit
	}
	lines := make([]string, 0, size)
	for i := 0; i < size; i++ {
		lines = append(lines, ring[(start+i)%limit])
	}
	return lines, nil
}

func loadAuditSystemLogView(provider auditRouteProvider, limit int) (map[string]any, error) {
	logDir := resolveAuditSystemLogDir(provider)
	path, err := findAuditSystemLogFile(logDir)
	if err != nil {
		if errors.Is(err, errAuditSystemLogNotFound) {
			return map[string]any{
				"available": false,
				"path":      "",
				"dir":       filepath.Clean(logDir),
				"lines":     []string{},
				"limit":     limit,
				"message":   "日志文件不存在，请确认程序已写入运行日志。",
			}, nil
		}
		return nil, err
	}

	lines, err := readAuditSystemLogTail(path, limit)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"available": true,
		"path":      filepath.Clean(path),
		"dir":       filepath.Clean(logDir),
		"lines":     lines,
		"limit":     limit,
	}, nil
}

func recordAdminAudit(provider adminAuditProvider, r *http.Request, category, action, target, result, summary, detail, username string) {
	if provider == nil {
		return
	}
	provider.RecordAuditLog(runtime.AuditLogEntry{
		At:         time.Now(),
		Category:   strings.TrimSpace(category),
		Action:     strings.TrimSpace(action),
		Target:     strings.TrimSpace(target),
		Result:     strings.TrimSpace(result),
		Summary:    strings.TrimSpace(summary),
		Detail:     strings.TrimSpace(detail),
		Username:   resolveAuditUsername(provider, username),
		RemoteAddr: resolveAuditRemoteAddr(r),
		Method:     strings.TrimSpace(r.Method),
		Path:       strings.TrimSpace(r.URL.Path),
	})
}

func resolveAuditUsername(provider adminAuditProvider, fallback string) string {
	_ = provider
	return strings.TrimSpace(fallback)
}

func resolveAuditRemoteAddr(r *http.Request) string {
	if r == nil {
		return ""
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && strings.TrimSpace(host) != "" {
		return strings.TrimSpace(host)
	}
	return strings.TrimSpace(r.RemoteAddr)
}
