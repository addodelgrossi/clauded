package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/addodelgrossi/clauded/internal/config"
	"github.com/addodelgrossi/clauded/internal/runner"
	"github.com/addodelgrossi/clauded/internal/session"
)

// fakeExecutor simula o claude para os testes de handler.
type fakeExecutor struct{ stdout string }

func (f fakeExecutor) Run(_ context.Context, _ runner.CommandSpec) ([]byte, error) {
	return []byte(f.stdout), nil
}

func (f fakeExecutor) Stream(_ context.Context, _ runner.CommandSpec) (io.ReadCloser, func() error, error) {
	return io.NopCloser(strings.NewReader(f.stdout)), func() error { return nil }, nil
}

func newTestServer(t *testing.T, root string) http.Handler {
	t.Helper()
	cfg := config.Config{
		Addr:               "127.0.0.1:0",
		AllowedRoots:       []string{root},
		MaxConcurrency:     2,
		DefaultModel:       "sonnet",
		ClaudeBin:          "claude",
		LogFormat:          "text",
		APIToken:           "secret-token",
		OAuthToken:         "oauth",
		RateLimitPerMinute: 0,
		SessionStore:       filepath.Join(t.TempDir(), "s.json"),
	}
	store, err := session.Open(cfg.SessionStore)
	if err != nil {
		t.Fatal(err)
	}
	fake := fakeExecutor{stdout: `{"type":"result","subtype":"success","result":"ok","session_id":"sid-1","num_turns":1}`}
	r := runner.New(cfg.ClaudeBin, cfg.OAuthToken, "",
		runner.WithExecutor(fake), runner.WithDefaults(runner.Defaults{Model: cfg.DefaultModel}))
	srv := New(cfg, WithRunner(r), WithSessionStore(store))
	return srv.Handler()
}

func TestAuth(t *testing.T) {
	h := newTestServer(t, t.TempDir())

	// Sem token -> 401.
	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("sem token: status %d, quer 401", rec.Code)
	}

	// Token errado -> 401.
	req = httptest.NewRequest(http.MethodGet, "/version", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("token errado: status %d, quer 401", rec.Code)
	}

	// Token certo -> 200.
	req = httptest.NewRequest(http.MethodGet, "/version", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("token certo: status %d, quer 200", rec.Code)
	}

	// /healthz é público.
	req = httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("healthz: status %d, quer 200", rec.Code)
	}
}

func TestRuns_WorkdirAllowlist(t *testing.T) {
	root := t.TempDir()
	h := newTestServer(t, root)

	// workdir fora da allowlist -> 403.
	body := `{"prompt":"oi","workdir":"/etc"}`
	rec := doRun(h, body)
	if rec.Code != http.StatusForbidden {
		t.Errorf("workdir /etc: status %d, quer 403 (body=%s)", rec.Code, rec.Body.String())
	}

	// workdir dentro da allowlist -> 200.
	rec = doRun(h, `{"prompt":"oi","workdir":"`+root+`"}`)
	if rec.Code != http.StatusOK {
		t.Errorf("workdir permitido: status %d, quer 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var res runner.Result
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Result != "ok" {
		t.Errorf("result = %q, quer ok", res.Result)
	}
}

func TestRuns_DangerousModeBlocked(t *testing.T) {
	root := t.TempDir()
	h := newTestServer(t, root)
	rec := doRun(h, `{"prompt":"oi","workdir":"`+root+`","permission_mode":"bypassPermissions"}`)
	if rec.Code != http.StatusForbidden {
		t.Errorf("bypassPermissions: status %d, quer 403", rec.Code)
	}
}

func TestRuns_BareRequiresAPIKey(t *testing.T) {
	root := t.TempDir()
	h := newTestServer(t, root)
	rec := doRun(h, `{"prompt":"oi","workdir":"`+root+`","bare":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bare sem api key: status %d, quer 400", rec.Code)
	}
}

func TestRuns_InvalidJSON(t *testing.T) {
	root := t.TempDir()
	h := newTestServer(t, root)
	rec := doRun(h, `{bad json`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("json inválido: status %d, quer 400", rec.Code)
	}
}

func TestRateLimit(t *testing.T) {
	root := t.TempDir()
	cfg := config.Config{
		Addr: "127.0.0.1:0", AllowedRoots: []string{root}, MaxConcurrency: 2,
		DefaultModel: "sonnet", ClaudeBin: "claude", LogFormat: "text",
		APIToken: "secret-token", OAuthToken: "oauth",
		RateLimitPerMinute: 1, SessionStore: filepath.Join(t.TempDir(), "s.json"),
	}
	store, _ := session.Open(cfg.SessionStore)
	srv := New(cfg, WithSessionStore(store))
	h := srv.Handler()

	first := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	h.ServeHTTP(first, req)
	if first.Code != http.StatusOK {
		t.Fatalf("primeira req: %d", first.Code)
	}

	second := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/version", nil)
	req2.Header.Set("Authorization", "Bearer secret-token")
	h.ServeHTTP(second, req2)
	if second.Code != http.StatusTooManyRequests {
		t.Errorf("segunda req: status %d, quer 429", second.Code)
	}
}

func TestValidatePath(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	srv := New(config.Config{AllowedRoots: []string{root}, MaxConcurrency: 1})

	if _, err := srv.validatePath(sub); err != nil {
		t.Errorf("subdir deveria ser permitido: %v", err)
	}
	if _, err := srv.validatePath("/etc"); err == nil {
		t.Errorf("/etc deveria ser rejeitado")
	}
	// path traversal para fora da raiz.
	if _, err := srv.validatePath(filepath.Join(root, "..", "..", "etc")); err == nil {
		t.Errorf("traversal deveria ser rejeitado")
	}
}

func doRun(h http.Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/runs", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
