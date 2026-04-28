package webui

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestHandlerMountRootUsesVueByDefault(t *testing.T) {
	h, err := NewHandler("/")
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}

	mux := http.NewServeMux()
	h.Mount(mux)

	rec := serveRequest(mux, http.MethodGet, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Go-bot Admin Console") {
		t.Fatalf("body = %s, want admin console title", body)
	}
	if !strings.Contains(body, `basePath: "/"`) || !strings.Contains(body, `vueBasePath: "/"`) {
		t.Fatalf("body = %s, want root Vue runtime paths", body)
	}
	if strings.Contains(body, "legacyEntryPath") {
		t.Fatalf("body = %s, want legacy runtime config removed", body)
	}

	assetPath := findVueAssetPath(t, body, "/")
	rec = serveRequest(mux, http.MethodGet, assetPath)
	if rec.Code != http.StatusOK {
		t.Fatalf("asset status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("cache-control = %q, want immutable asset cache", rec.Header().Get("Cache-Control"))
	}
}

func TestHandlerMountSubpathUsesVueByDefault(t *testing.T) {
	h, err := NewHandler("/console")
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}

	mux := http.NewServeMux()
	h.Mount(mux)

	rec := serveRequest(mux, http.MethodGet, "/console/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Go-bot Admin Console") {
		t.Fatalf("body = %s, want admin console title", body)
	}
	if !strings.Contains(body, `basePath: "/console"`) || !strings.Contains(body, `vueBasePath: "/console"`) {
		t.Fatalf("body = %s, want subpath admin runtime paths", body)
	}
	if strings.Contains(body, "legacyEntryPath") {
		t.Fatalf("body = %s, want legacy runtime config removed", body)
	}

	assetPath := findVueAssetPath(t, body, "/console/")
	rec = serveRequest(mux, http.MethodGet, assetPath)
	if rec.Code != http.StatusOK {
		t.Fatalf("asset status = %d, want 200", rec.Code)
	}

	rec = serveRequest(mux, http.MethodGet, "/console")
	if rec.Code != http.StatusTemporaryRedirect {
		t.Fatalf("redirect status = %d, want 307", rec.Code)
	}
	if location := rec.Header().Get("Location"); location != "/console/" {
		t.Fatalf("location = %q, want /console/", location)
	}
}

func TestHandlerMountRemovedRoutesReturnNotFound(t *testing.T) {
	rootHandler, err := NewHandler("/")
	if err != nil {
		t.Fatalf("NewHandler(root) error = %v", err)
	}
	rootMux := http.NewServeMux()
	rootHandler.Mount(rootMux)
	for _, target := range []string{
		"/legacy/",
		"/legacy/assets/style.css",
		"/v2/",
	} {
		rec := serveRequest(rootMux, http.MethodGet, target)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("root target %q status = %d, want 404", target, rec.Code)
		}
	}

	subpathHandler, err := NewHandler("/console")
	if err != nil {
		t.Fatalf("NewHandler(subpath) error = %v", err)
	}
	mux := http.NewServeMux()
	subpathHandler.Mount(mux)
	for _, target := range []string{
		"/console/legacy/",
		"/console/legacy/assets/style.css",
		"/console/v2/",
	} {
		rec := serveRequest(mux, http.MethodGet, target)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("subpath target %q status = %d, want 404", target, rec.Code)
		}
	}
}

func serveRequest(mux *http.ServeMux, method, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func findVueAssetPath(t *testing.T, body, entryBase string) string {
	t.Helper()
	assetPattern := regexp.MustCompile(`\./assets/[^\"]+\.js`)
	assetMatch := assetPattern.FindString(body)
	if assetMatch == "" {
		t.Fatalf("body = %s, want compiled Vue asset reference", body)
	}
	return entryBase + strings.TrimPrefix(assetMatch, "./")
}
