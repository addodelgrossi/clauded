package runner

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// argValue devolve o valor que segue a flag name em args, ou "" se ausente.
func argValue(args []string, name string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func hasFlag(args []string, name string) bool {
	return slices.Contains(args, name)
}

func TestBuildArgs_Defaults(t *testing.T) {
	args, sid, err := BuildArgs(RunRequest{Prompt: "oi"}, Defaults{Model: "sonnet"})
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if !hasFlag(args, "-p") {
		t.Errorf("esperava -p em %v", args)
	}
	if got := argValue(args, "-p"); got != "oi" {
		t.Errorf("prompt = %q, quer oi", got)
	}
	if got := argValue(args, "--output-format"); got != "json" {
		t.Errorf("output-format = %q, quer json", got)
	}
	if got := argValue(args, "--model"); got != "sonnet" {
		t.Errorf("model default = %q, quer sonnet", got)
	}
	if got := argValue(args, "--session-id"); got != sid {
		t.Errorf("session-id = %q, sid retornado = %q", got, sid)
	}
	if _, err := uuid.Parse(sid); err != nil {
		t.Errorf("session-id gerado não é UUID: %v", err)
	}
}

func TestBuildArgs_StreamForcesStreamJSON(t *testing.T) {
	args, _, err := BuildArgs(RunRequest{Prompt: "x", Stream: true}, Defaults{})
	if err != nil {
		t.Fatal(err)
	}
	if got := argValue(args, "--output-format"); got != "stream-json" {
		t.Errorf("output-format = %q, quer stream-json", got)
	}
	if !hasFlag(args, "--verbose") {
		t.Errorf("stream-json deve incluir --verbose: %v", args)
	}
}

func TestBuildArgs_ResumeAndContinue(t *testing.T) {
	id := uuid.NewString()
	args, sid, err := BuildArgs(RunRequest{Prompt: "x", Resume: id}, Defaults{})
	if err != nil {
		t.Fatal(err)
	}
	if got := argValue(args, "--resume"); got != id {
		t.Errorf("resume = %q, quer %q", got, id)
	}
	if sid != id {
		t.Errorf("sid = %q, quer %q", sid, id)
	}
	if hasFlag(args, "--session-id") {
		t.Errorf("resume não deve gerar --session-id: %v", args)
	}

	args2, sid2, err := BuildArgs(RunRequest{Prompt: "x", Continue: true}, Defaults{})
	if err != nil {
		t.Fatal(err)
	}
	if !hasFlag(args2, "--continue") {
		t.Errorf("esperava --continue: %v", args2)
	}
	if sid2 != "" {
		t.Errorf("continue não deve fixar session_id, got %q", sid2)
	}
}

func TestBuildArgs_AllFlags(t *testing.T) {
	req := RunRequest{
		Prompt:             "faça algo",
		Workdir:            "/tmp/proj",
		Model:              "opus",
		FallbackModel:      "sonnet",
		Fork:               true,
		SessionID:          uuid.NewString(),
		PermissionMode:     "acceptEdits",
		Tools:              "Bash,Edit,Read",
		MaxTurns:           5,
		MaxBudgetUSD:       1.5,
		SystemPrompt:       "sys",
		AppendSystemPrompt: "append",
		StrictMCPConfig:    true,
		Bare:               true,
		Effort:             "high",
		SettingSources:     "user,project",
		PluginDirs:         []string{"/tmp/p1", "/tmp/p2"},
		PluginURLs:         []string{"https://x/y.zip"},
		MCPConfig:          []byte(`{"a":1}`),
		Agents:             []byte(`{"r":{}}`),
		JSONSchema:         []byte(`{"type":"object"}`),
	}
	args, _, err := BuildArgs(req, Defaults{Model: "sonnet"})
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string]string{
		"--model":                "opus",
		"--fallback-model":       "sonnet",
		"--add-dir":              "/tmp/proj",
		"--permission-mode":      "acceptEdits",
		"--tools":                "Bash,Edit,Read",
		"--max-turns":            "5",
		"--max-budget-usd":       "1.5",
		"--system-prompt":        "sys",
		"--append-system-prompt": "append",
		"--effort":               "high",
		"--setting-sources":      "user,project",
		"--mcp-config":           `{"a":1}`,
		"--agents":               `{"r":{}}`,
		"--json-schema":          `{"type":"object"}`,
	}
	for flag, want := range checks {
		if got := argValue(args, flag); got != want {
			t.Errorf("%s = %q, quer %q", flag, got, want)
		}
	}
	for _, f := range []string{"--fork-session", "--strict-mcp-config", "--bare"} {
		if !hasFlag(args, f) {
			t.Errorf("esperava flag %s em %v", f, args)
		}
	}
	// plugin-dir/url repetíveis
	if c := strings.Count(strings.Join(args, " "), "--plugin-dir"); c != 2 {
		t.Errorf("esperava 2 --plugin-dir, got %d", c)
	}
	if c := strings.Count(strings.Join(args, " "), "--plugin-url"); c != 1 {
		t.Errorf("esperava 1 --plugin-url, got %d", c)
	}
}

func TestBuildArgs_Validation(t *testing.T) {
	cases := []struct {
		name string
		req  RunRequest
	}{
		{"sem prompt", RunRequest{}},
		{"output_format inválido", RunRequest{Prompt: "x", OutputFormat: "xml"}},
		{"permission_mode inválido", RunRequest{Prompt: "x", PermissionMode: "nope"}},
		{"effort inválido", RunRequest{Prompt: "x", Effort: "ultra"}},
		{"session_id não-uuid", RunRequest{Prompt: "x", SessionID: "abc"}},
		{"resume não-uuid", RunRequest{Prompt: "x", Resume: "abc"}},
		{"continue+resume", RunRequest{Prompt: "x", Continue: true, Resume: uuid.NewString()}},
		{"max_turns negativo", RunRequest{Prompt: "x", MaxTurns: -1}},
		{"json_schema inválido", RunRequest{Prompt: "x", JSONSchema: []byte(`{bad`)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, _, err := BuildArgs(c.req, Defaults{}); err == nil {
				t.Errorf("esperava erro para %s", c.name)
			}
		})
	}
}
