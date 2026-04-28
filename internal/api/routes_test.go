package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

var referenceMethodRe = regexp.MustCompile(`export\s+async\s+function\s+(GET|POST|PUT|PATCH|DELETE)\b`)

// TestRouteInventory creates a snapshot of all registered routes.
// This test ensures route changes are intentional and visible in diffs.
func TestRouteInventory(t *testing.T) {
	server, _, cleanup := newTestServer(t)
	defer cleanup()
	handler := server.Handler()

	// Extract routes from chi router
	routes := extractRoutes(handler)
	sort.Strings(routes)

	// Expected routes based on current implementation
	// Note: Chi adds HEAD/POST/PUT/DELETE for redirect handlers automatically
	expected := []string{
		"DELETE /",
		"DELETE /api/cli-tools/antigravity-mitm",
		"DELETE /api/cli-tools/claude-settings",
		"DELETE /api/cli-tools/codex-settings",
		"DELETE /api/cli-tools/copilot-settings",
		"DELETE /api/cli-tools/droid-settings",
		"DELETE /api/cli-tools/hermes-settings",
		"DELETE /api/cli-tools/openclaw-settings",
		"DELETE /api/cli-tools/opencode-settings",
		"DELETE /api/cli-tools/{tool}-settings",
		"DELETE /api/combos/{name}",
		"DELETE /api/keys/{id}",
		"DELETE /api/models/alias",
		"DELETE /api/models/alias/{name}",
		"DELETE /api/models/custom",
		"DELETE /api/models/custom/{name}",
		"DELETE /api/oauth/tokens/{provider}/{account}",
		"DELETE /api/pricing",
		"DELETE /api/provider-nodes/{id}",
		"DELETE /api/providers/{name}",
		"DELETE /api/proxy-pools/{id}",
		"DELETE /api/translator/console-logs",
		"DELETE /dashboard",
		"DELETE /ui",
		"DELETE /ui/*",
		"GET /",
		"GET /api/cli-tools/antigravity-mitm",
		"GET /api/cli-tools/antigravity-mitm/alias",
		"GET /api/cli-tools/claude-settings",
		"GET /api/cli-tools/codex-settings",
		"GET /api/cli-tools/copilot-settings",
		"GET /api/cli-tools/droid-settings",
		"GET /api/cli-tools/hermes-settings",
		"GET /api/cli-tools/openclaw-settings",
		"GET /api/cli-tools/opencode-settings",
		"GET /api/cli-tools/{tool}-settings",
		"GET /api/cloud/models/alias",
		"GET /api/combos",
		"GET /api/combos/{name}",
		"GET /api/config",
		"GET /api/config/export",
		"GET /api/health",
		"GET /api/init",
		"GET /api/keys",
		"GET /api/keys/{id}",
		"GET /api/logs",
		"GET /api/media-providers/tts/elevenlabs/voices",
		"GET /api/media-providers/tts/voices",
		"GET /api/metrics",
		"GET /api/metrics/json",
		"GET /api/models",
		"GET /api/models/alias",
		"GET /api/models/availability",
		"GET /api/models/custom",
		"GET /api/nodes",
		"GET /api/oauth/authorize",
		"GET /api/oauth/callback",
		"GET /api/oauth/cursor/auto-import",
		"GET /api/oauth/cursor/import",
		"GET /api/oauth/kiro/auto-import",
		"GET /api/oauth/kiro/social-authorize",
		"GET /api/oauth/poll/{provider}",
		"GET /api/oauth/tokens",
		"GET /api/oauth/{provider}/{action}",
		"GET /api/pricing",
		"GET /api/provider-nodes",
		"GET /api/provider-nodes/{id}",
		"GET /api/providers",
		"GET /api/providers/catalog",
		"GET /api/providers/client",
		"GET /api/providers/kilo/free-models",
		"GET /api/providers/suggested-models",
		"GET /api/providers/{name}",
		"GET /api/providers/{name}/accounts/{account}/health",
		"GET /api/providers/{name}/health",
		"GET /api/providers/{name}/models",
		"GET /api/proxy-pools",
		"GET /api/proxy-pools/{id}",
		"GET /api/settings",
		"GET /api/settings/database",
		"GET /api/settings/require-login",
		"GET /api/setup/status",
		"GET /api/sync/status",
		"GET /api/tags",
		"GET /api/translator/console-logs",
		"GET /api/translator/console-logs/stream",
		"GET /api/translator/load",
		"GET /api/tunnel/status",
		"GET /api/tunnel/tailscale-check",
		"GET /api/usage",
		"GET /api/usage/chart",
		"GET /api/usage/history",
		"GET /api/usage/logs",
		"GET /api/usage/providers",
		"GET /api/usage/request-details",
		"GET /api/usage/request-logs",
		"GET /api/usage/stats",
		"GET /api/usage/stream",
		"GET /api/usage/{connectionId}",
		"GET /api/v1",
		"GET /api/v1/models",
		"GET /api/v1beta/models",
		"GET /api/v1beta/models/{model}",
		"GET /api/version",
		"GET /dashboard",
		"GET /healthz",
		"GET /metrics",
		"GET /readyz",
		"GET /ui",
		"GET /ui/*",
		"GET /v1/models",
		"GET /v1beta/models",
		"GET /v1beta/models/{model}",
		"HEAD /",
		"HEAD /dashboard",
		"HEAD /ui",
		"HEAD /ui/*",
		"POST /",
		"POST /api/auth/login",
		"POST /api/auth/logout",
		"POST /api/cli-tools/antigravity-mitm",
		"POST /api/cli-tools/claude-settings",
		"POST /api/cli-tools/codex-settings",
		"POST /api/cli-tools/copilot-settings",
		"POST /api/cli-tools/droid-settings",
		"POST /api/cli-tools/hermes-settings",
		"POST /api/cli-tools/openclaw-settings",
		"POST /api/cli-tools/opencode-settings",
		"POST /api/cli-tools/{tool}-settings",
		"POST /api/cloud/auth",
		"POST /api/cloud/model/resolve",
		"POST /api/combos",
		"POST /api/config/import",
		"POST /api/keys",
		"POST /api/locale",
		"POST /api/models/alias",
		"POST /api/models/availability",
		"POST /api/models/custom",
		"POST /api/models/test",
		"POST /api/oauth/exchange",
		"POST /api/oauth/cursor/import",
		"POST /api/oauth/gitlab/pat",
		"POST /api/oauth/iflow/cookie",
		"POST /api/oauth/kiro/import",
		"POST /api/oauth/kiro/social-exchange",
		"POST /api/oauth/{provider}/{action}",
		"POST /api/provider-nodes",
		"POST /api/provider-nodes/validate",
		"POST /api/providers",
		"POST /api/providers/test-batch",
		"POST /api/providers/validate",
		"POST /api/providers/{name}/test",
		"POST /api/providers/{name}/test-models",
		"POST /api/proxy-pools",
		"POST /api/proxy-pools/vercel-deploy",
		"POST /api/proxy-pools/{id}/test",
		"POST /api/settings/database",
		"POST /api/settings/proxy-test",
		"POST /api/shutdown",
		"POST /api/translator/save",
		"POST /api/translator/send",
		"POST /api/translator/translate",
		"POST /api/tunnel/disable",
		"POST /api/tunnel/enable",
		"POST /api/tunnel/tailscale-disable",
		"POST /api/tunnel/tailscale-enable",
		"POST /api/tunnel/tailscale-install",
		"POST /api/tunnel/tailscale-login",
		"POST /api/tunnel/tailscale-start-daemon",
		"POST /api/v1/api/chat",
		"POST /api/v1/audio/speech",
		"POST /api/v1/audio/transcriptions",
		"POST /api/v1/chat/completions",
		"POST /api/v1/embeddings",
		"POST /api/v1/images/generations",
		"POST /api/v1/messages",
		"POST /api/v1/messages/count_tokens",
		"POST /api/v1/responses",
		"POST /api/v1/responses/compact",
		"POST /api/v1/web/fetch",
		"POST /api/v1/web/search",
		"POST /api/v1beta/models/{path:.*}",
		"POST /api/version/update",
		"POST /codex/v1/responses",
		"POST /codex/{path}",
		"POST /dashboard",
		"POST /ui",
		"POST /ui/*",
		"POST /v1/api/chat",
		"POST /v1/audio/speech",
		"POST /v1/audio/transcriptions",
		"POST /v1/chat/completions",
		"POST /v1/embeddings",
		"POST /v1/images/generations",
		"POST /v1/messages",
		"POST /v1/messages/count_tokens",
		"POST /v1/responses",
		"POST /v1/responses/compact",
		"POST /v1/web/fetch",
		"POST /v1/web/search",
		"POST /v1beta/models/{path:.*}",
		"PUT /",
		"PUT /api/cli-tools/antigravity-mitm/alias",
		"PUT /api/cloud/credentials/update",
		"PUT /api/cloud/models/alias",
		"PUT /api/combos/{name}",
		"PUT /api/keys/{id}",
		"PUT /api/models",
		"PUT /api/models/alias",
		"PUT /api/models/alias/{name}",
		"PUT /api/models/custom/{name}",
		"PUT /api/provider-nodes/{id}",
		"PUT /api/providers/{name}",
		"PUT /api/proxy-pools/{id}",
		"PUT /api/settings",
		"PUT /dashboard",
		"PUT /ui",
		"PUT /ui/*",
	}

	// Compare
	if len(routes) != len(expected) {
		t.Errorf("Route count mismatch: got %d, want %d", len(routes), len(expected))
		t.Logf("Missing or extra routes detected")
	}

	missing := []string{}
	extra := []string{}

	expectedMap := make(map[string]bool)
	for _, r := range expected {
		expectedMap[r] = true
	}

	routesMap := make(map[string]bool)
	for _, r := range routes {
		routesMap[r] = true
		if !expectedMap[r] {
			extra = append(extra, r)
		}
	}

	for _, r := range expected {
		if !routesMap[r] {
			missing = append(missing, r)
		}
	}

	if len(missing) > 0 {
		t.Errorf("Missing routes:\n  %s", strings.Join(missing, "\n  "))
	}

	if len(extra) > 0 {
		t.Errorf("Extra routes:\n  %s", strings.Join(extra, "\n  "))
	}

	// Log all routes for reference
	if t.Failed() {
		t.Logf("Current routes:\n  %s", strings.Join(routes, "\n  "))
	}
}

func TestReferenceAPIRouteParity(t *testing.T) {
	server, _, cleanup := newTestServer(t)
	defer cleanup()

	referenceRoot := filepath.Join("..", "..", "reference", "9router", "src", "app", "api")
	entries := []string{}
	err := filepath.WalkDir(referenceRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == "route.js" {
			entries = append(entries, path)
		}
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("reference routes not available under %s", referenceRoot)
		}
		t.Fatalf("walk reference routes: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("no reference route.js files found under %s", referenceRoot)
	}

	actual := map[string]bool{}
	_ = chi.Walk(server.Handler().(*chi.Mux), func(method string, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method == "CONNECT" || method == "OPTIONS" || method == "TRACE" {
			return nil
		}
		actual[method+" "+route] = true
		return nil
	})

	missing := []string{}
	for _, file := range entries {
		rel, err := filepath.Rel(referenceRoot, file)
		if err != nil {
			t.Fatalf("rel route: %v", err)
		}
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		methods := referenceMethodRe.FindAllStringSubmatch(string(content), -1)
		if len(methods) == 0 {
			t.Fatalf("reference route %s declares no HTTP methods", rel)
		}
		path := referenceRouteToGoPath(rel)
		for _, match := range methods {
			key := match[1] + " " + path
			if !actual[key] {
				missing = append(missing, key+" (from "+rel+")")
			}
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("missing reference API routes:\n  %s", strings.Join(missing, "\n  "))
	}
}

func referenceRouteToGoPath(rel string) string {
	rel = filepath.ToSlash(rel)
	rel = strings.TrimSuffix(rel, "/route.js")
	switch rel {
	case "combos/[id]":
		return "/api/combos/{name}"
	case "keys/[id]":
		return "/api/keys/{id}"
	case "provider-nodes/[id]":
		return "/api/provider-nodes/{id}"
	case "providers/[id]":
		return "/api/providers/{name}"
	case "providers/[id]/models":
		return "/api/providers/{name}/models"
	case "providers/[id]/test":
		return "/api/providers/{name}/test"
	case "providers/[id]/test-models":
		return "/api/providers/{name}/test-models"
	case "proxy-pools/[id]":
		return "/api/proxy-pools/{id}"
	case "proxy-pools/[id]/test":
		return "/api/proxy-pools/{id}/test"
	case "usage/[connectionId]":
		return "/api/usage/{connectionId}"
	case "oauth/[provider]/[action]":
		return "/api/oauth/{provider}/{action}"
	case "v1beta/models/[...path]":
		return "/api/v1beta/models/{path:.*}"
	}
	return "/api/" + rel
}

// extractRoutes walks the chi router and extracts all registered routes
func extractRoutes(handler http.Handler) []string {
	routes := []string{}
	seen := make(map[string]bool)

	chiRouter, ok := handler.(*chi.Mux)
	if !ok {
		return routes
	}

	walkFunc := func(method string, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		// Filter out methods we don't explicitly register
		// Chi adds all methods for redirect handlers, but we only care about intentional routes
		if method == "CONNECT" || method == "OPTIONS" || method == "TRACE" || method == "PATCH" {
			return nil
		}

		key := method + " " + route
		if !seen[key] {
			seen[key] = true
			routes = append(routes, key)
		}
		return nil
	}

	_ = chi.Walk(chiRouter, walkFunc)
	return routes
}

// TestRouteHandlerExists ensures critical routes have non-nil handlers
func TestRouteHandlerExists(t *testing.T) {
	server, _, cleanup := newTestServer(t)
	defer cleanup()
	handler := server.Handler()

	criticalRoutes := []struct {
		method string
		path   string
	}{
		{"GET", "/healthz"},
		{"GET", "/readyz"},
		{"GET", "/v1/models"},
		{"POST", "/v1/chat/completions"},
		{"POST", "/v1/messages"},
		{"GET", "/api/providers"},
		{"GET", "/api/settings"},
	}

	for _, route := range criticalRoutes {
		req := httptest.NewRequest(route.method, route.path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		// We don't care about auth failures here, just that the route exists
		if rec.Code == http.StatusNotFound {
			t.Errorf("Route %s %s returned 404, handler may be missing", route.method, route.path)
		}
	}
}
