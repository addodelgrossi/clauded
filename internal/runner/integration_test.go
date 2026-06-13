//go:build integration

// Teste de integração que invoca o binário claude REAL. Rode com:
//
//	CLAUDE_CODE_OAUTH_TOKEN=... go test -tags=integration ./internal/runner -run Integration -v
//
// Requer claude instalado no PATH e o token da assinatura no ambiente.
// Também cobre a verificação do flag --max-turns (que pode não existir em
// todas as versões do CLI).
package runner

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestIntegration_RunReal(t *testing.T) {
	token := os.Getenv("CLAUDE_CODE_OAUTH_TOKEN")
	if token == "" {
		t.Skip("CLAUDE_CODE_OAUTH_TOKEN ausente; pulando teste de integração")
	}
	r := New("claude", token, "", WithDefaults(Defaults{Model: "haiku"}))

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	wd, _ := os.Getwd()
	res, sid, err := r.Run(ctx, RunRequest{
		Prompt:   "Responda apenas com a palavra: pong",
		Workdir:  wd,
		MaxTurns: 1, // exercita --max-turns
	})
	if err != nil {
		t.Fatalf("run real falhou: %v", err)
	}
	if sid == "" {
		t.Errorf("session_id vazio")
	}
	t.Logf("resultado: %q (session=%s, turns=%d, cost=%.4f)", res.Result, sid, res.NumTurns, res.TotalCostUSD)
}
