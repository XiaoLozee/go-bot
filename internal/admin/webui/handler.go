package webui

import (
	"embed"
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
)

const (
	vueDistDir                = "frontend/dist"
	legacyRouteSegment        = "legacy"
	deprecatedVueRouteSegment = "v2"
	webUIBasePathPlaceholder  = "__GOBOT_WEBUI_BASE_PATH__"
	vueBasePathPlaceholder    = "__GOBOT_VUE_BASE_PATH__"
)

//go:embed all:frontend/dist
var assetFS embed.FS

type staticAsset struct {
	body        []byte
	contentType string
	immutable   bool
}

type Handler struct {
	vueBasePath string
	vueIndex    []byte
	vueAssets   map[string]staticAsset
}

func NewHandler(basePath string) (*Handler, error) {
	basePath = normalizeBasePath(basePath)
	vueIndex, vueAssets, err := loadVueBundle(basePath)
	if err != nil {
		return nil, err
	}
	return &Handler{
		vueBasePath: basePath,
		vueIndex:    vueIndex,
		vueAssets:   vueAssets,
	}, nil
}

func loadVueBundle(basePath string) ([]byte, map[string]staticAsset, error) {
	distFS, err := fs.Sub(assetFS, vueDistDir)
	if err != nil {
		return nil, nil, err
	}

	assets := make(map[string]staticAsset)
	var index []byte
	fileCount := 0
	walkErr := fs.WalkDir(distFS, ".", func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		fileCount++
		payload, err := fs.ReadFile(distFS, filePath)
		if err != nil {
			return err
		}
		if filePath == "index.html" {
			index = injectVueRuntimeConfig(payload, basePath)
			return nil
		}
		assets[filePath] = staticAsset{
			body:        payload,
			contentType: buildContentType(filePath, payload),
			immutable:   strings.HasPrefix(filePath, "assets/"),
		}
		return nil
	})
	if walkErr != nil {
		return nil, nil, walkErr
	}
	if fileCount == 0 || len(index) == 0 {
		return nil, nil, errors.New("vue dist bundle is empty")
	}
	return index, assets, nil
}

func injectVueRuntimeConfig(content []byte, basePath string) []byte {
	indexHTML := string(content)
	indexHTML = strings.ReplaceAll(indexHTML, webUIBasePathPlaceholder, strconv.Quote(basePath))
	indexHTML = strings.ReplaceAll(indexHTML, vueBasePathPlaceholder, strconv.Quote(basePath))
	return []byte(indexHTML)
}

func buildContentType(name string, payload []byte) string {
	if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
		if strings.Contains(contentType, "charset=") {
			return contentType
		}
		if strings.HasPrefix(contentType, "text/") || strings.Contains(contentType, "javascript") || strings.Contains(contentType, "json") {
			return contentType + "; charset=utf-8"
		}
		return contentType
	}
	return http.DetectContentType(payload)
}

func (h *Handler) Mount(mux *http.ServeMux) {
	h.mountVue(mux)
}

func (h *Handler) mountVue(mux *http.ServeMux) {
	h.mountVueAt(mux, h.vueBasePath)
}

func (h *Handler) mountVueAt(mux *http.ServeMux, routeBase string) {
	routeBase = normalizeBasePath(routeBase)
	pattern := vueBasePathWithSlash(routeBase)
	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		h.serveVueAt(w, r, routeBase)
	})
	if routeBase != "/" {
		mux.HandleFunc(routeBase, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			http.Redirect(w, r, routeBase+"/", http.StatusTemporaryRedirect)
		})
	}
}

func (h *Handler) serveVueAt(w http.ResponseWriter, r *http.Request, routeBase string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	routeBase = normalizeBasePath(routeBase)
	if isRemovedRouteRequest(r.URL.Path, routeBase) {
		http.NotFound(w, r)
		return
	}

	relPath := strings.TrimPrefix(r.URL.Path, routeBase)
	if routeBase == "/" {
		relPath = strings.TrimPrefix(r.URL.Path, "/")
	}
	relPath = strings.TrimPrefix(relPath, "/")
	relPath = strings.TrimSpace(relPath)
	if relPath == "" || relPath == "index.html" {
		h.serveVueIndex(w)
		return
	}

	if asset, ok := h.vueAssets[relPath]; ok {
		w.Header().Set("Content-Type", asset.contentType)
		if asset.immutable {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=300")
		}
		_, _ = w.Write(asset.body)
		return
	}

	if path.Ext(relPath) != "" {
		http.NotFound(w, r)
		return
	}

	h.serveVueIndex(w)
}

func (h *Handler) serveVueIndex(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(h.vueIndex)
}

func vueBasePathWithSlash(basePath string) string {
	basePath = normalizeBasePath(basePath)
	if basePath == "/" {
		return "/"
	}
	return basePath + "/"
}

func isRemovedRouteRequest(requestPath, routeBase string) bool {
	requestPath = normalizeBasePath(requestPath)
	for _, segment := range []string{legacyRouteSegment, deprecatedVueRouteSegment} {
		removedPath := appendBasePathSegment(routeBase, segment)
		if requestPath == removedPath || strings.HasPrefix(requestPath, removedPath+"/") {
			return true
		}
	}
	return false
}

func appendBasePathSegment(basePath, segment string) string {
	segment = strings.Trim(segment, "/")
	if segment == "" {
		return normalizeBasePath(basePath)
	}
	basePath = normalizeBasePath(basePath)
	if basePath == "/" {
		return "/" + segment
	}
	return basePath + "/" + segment
}

func normalizeBasePath(basePath string) string {
	basePath = strings.TrimSpace(basePath)
	if basePath == "" {
		return "/"
	}
	if !strings.HasPrefix(basePath, "/") {
		basePath = "/" + basePath
	}
	if basePath != "/" {
		basePath = strings.TrimSuffix(basePath, "/")
	}
	return basePath
}
