package runner

import (
	"context"
	"io"
	"strings"
	"testing"
)

// fakeExecutor simula o stdout do claude sem invocar o binário real.
type fakeExecutor struct {
	stdout   []byte
	err      error
	gotSpec  CommandSpec
	streamFn func() ([]byte, error)
}

func (f *fakeExecutor) Run(_ context.Context, spec CommandSpec) ([]byte, error) {
	f.gotSpec = spec
	return f.stdout, f.err
}

func (f *fakeExecutor) Stream(_ context.Context, spec CommandSpec) (io.ReadCloser, func() error, error) {
	f.gotSpec = spec
	return io.NopCloser(strings.NewReader(string(f.stdout))), func() error { return f.err }, nil
}

func TestRunner_Run(t *testing.T) {
	fake := &fakeExecutor{stdout: []byte(`{"type":"result","subtype":"success","is_error":false,"result":"pronto","session_id":"abc-123","num_turns":2,"duration_ms":1500,"total_cost_usd":0.01}`)}
	r := New("claude", "oauth-secret", "", WithExecutor(fake), WithDefaults(Defaults{Model: "sonnet"}))

	res, sid, err := r.Run(context.Background(), RunRequest{Prompt: "oi", Workdir: "/tmp/x"})
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if res.Result != "pronto" || res.NumTurns != 2 || res.TotalCostUSD != 0.01 {
		t.Errorf("resultado inesperado: %+v", res)
	}
	if sid == "" {
		t.Errorf("session id vazio")
	}
	// O env do subprocesso deve conter o token OAuth.
	if !containsEnv(fake.gotSpec.Env, "CLAUDE_CODE_OAUTH_TOKEN=oauth-secret") {
		t.Errorf("env não contém o token OAuth")
	}
	if fake.gotSpec.Dir != "/tmp/x" {
		t.Errorf("cwd = %q, quer /tmp/x", fake.gotSpec.Dir)
	}
}

func TestParseResult_LimitReached(t *testing.T) {
	res, err := ParseResult([]byte(`{"subtype":"error_max_turns","session_id":"s1","is_error":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if !res.LimitReached() {
		t.Errorf("esperava LimitReached true para error_max_turns")
	}
}

func containsEnv(env []string, want string) bool {
	for _, e := range env {
		if e == want {
			return true
		}
	}
	return false
}
