package main

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newDocsMux builds just the /docs and /docs/ routes from the same docsUI
// embed.FS main() uses, without the rest of main()'s wiring (DB, gRPC
// gateway, etc.) — those need a live config/Postgres and are exercised
// elsewhere; this only pins down the file-serving/redirect behavior.
func newDocsMux(t *testing.T) *http.ServeMux {
	t.Helper()
	docsAssets, err := fs.Sub(docsUI, "openapi/vendor/swagger-ui")
	if err != nil {
		t.Fatalf("fs.Sub: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/docs", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/docs/", http.StatusMovedPermanently)
	})
	mux.Handle("/docs/", http.StripPrefix("/docs/", http.FileServer(http.FS(docsAssets))))
	return mux
}

func TestDocsRedirectsWithoutTrailingSlash(t *testing.T) {
	mux := newDocsMux(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMovedPermanently)
	}
	if loc := rec.Header().Get("Location"); loc != "/docs/" {
		t.Errorf("Location = %q, want %q", loc, "/docs/")
	}
}

func TestDocsServesIndexHTML(t *testing.T) {
	mux := newDocsMux(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/docs/", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	// index.html points Swagger UI at this server's own spec, not the
	// swagger-ui-dist package's bundled Petstore demo.
	if !strings.Contains(body, "/openapi.json") {
		t.Errorf("index.html does not reference /openapi.json:\n%s", body)
	}
	if !strings.Contains(body, "swagger-ui-bundle.js") {
		t.Errorf("index.html does not load the vendored swagger-ui bundle:\n%s", body)
	}
}

func TestDocsServesVendoredAssets(t *testing.T) {
	mux := newDocsMux(t)
	for _, path := range []string{
		"/docs/assets/swagger-ui.css",
		"/docs/assets/swagger-ui-bundle.js",
		"/docs/assets/swagger-ui-standalone-preset.js",
	} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}
			if rec.Body.Len() == 0 {
				t.Error("response body is empty")
			}
		})
	}
}
