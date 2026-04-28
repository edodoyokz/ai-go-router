package webui

import (
	"io/fs"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerServesAssets(t *testing.T) {
	handler := Handler()
	jsAsset, cssAsset := findBuiltAssets(t)

	tests := []struct {
		path           string
		expectedStatus int
		expectedType   string
	}{
		{"/ui/" + jsAsset, 200, "javascript"},
		{"/ui/" + cssAsset, 200, "css"},
		{"/ui/", 200, "html"},
		{"/ui/nonexistent", 200, "html"}, // SPA fallback
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			if !strings.Contains(w.Header().Get("Content-Type"), tt.expectedType) {
				t.Errorf("expected %s in content-type, got %s", tt.expectedType, w.Header().Get("Content-Type"))
			}
		})
	}
}

func findBuiltAssets(t *testing.T) (string, string) {
	t.Helper()
	var jsAsset, cssAsset string
	distSubFS, err := fs.Sub(distFS, "dist")
	if err != nil {
		t.Fatal(err)
	}
	if err := fs.WalkDir(distSubFS, "assets", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".js") && jsAsset == "" {
			jsAsset = path
		}
		if strings.HasSuffix(path, ".css") && cssAsset == "" {
			cssAsset = path
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if jsAsset == "" || cssAsset == "" {
		t.Fatalf("expected built JS and CSS assets, got js=%q css=%q", jsAsset, cssAsset)
	}
	return jsAsset, cssAsset
}
