package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/addodelgrossi/clauded/internal/runner"
	"github.com/addodelgrossi/clauded/internal/version"
)

// errorBody é o envelope padronizado de erros da API.
type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, errorBody{Error: errorDetail{Code: code, Message: msg}})
}

// --- health / version ---

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, _ *http.Request) {
	var problems []string
	if _, err := exec.LookPath(s.cfg.ClaudeBin); err != nil {
		problems = append(problems, fmt.Sprintf("binário claude não encontrado (%s)", s.cfg.ClaudeBin))
	}
	if s.cfg.OAuthToken == "" && s.cfg.AnthropicAPIKey == "" {
		problems = append(problems, "token de autenticação ausente")
	}
	if len(problems) > 0 {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "not_ready", "problems": problems,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, version.Get())
}

func (s *Server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	// Placeholder textual no formato Prometheus; expandível conforme necessário.
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, "# clauded metrics\nclauded_up 1\nclauded_max_concurrency %d\n", s.cfg.MaxConcurrency)
}

// --- runs ---

func (s *Server) handleRuns(w http.ResponseWriter, r *http.Request) {
	log := s.logWith(r)

	var req runner.RunRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "corpo JSON inválido: "+err.Error())
		return
	}

	if err := req.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	// Política de segurança: permission modes perigosos.
	if runner.DangerousPermissionModes[req.PermissionMode] && !s.cfg.AllowDangerous {
		writeError(w, http.StatusForbidden, "dangerous_mode_disabled",
			fmt.Sprintf("permission_mode %q exige CLAUDED_ALLOW_DANGEROUS=true", req.PermissionMode))
		return
	}

	// bare quebra a auth OAuth: só é permitido se houver ANTHROPIC_API_KEY.
	if req.Bare && s.cfg.AnthropicAPIKey == "" {
		writeError(w, http.StatusBadRequest, "bare_requires_api_key",
			"bare=true desativa a auth OAuth; configure ANTHROPIC_API_KEY para usá-lo")
		return
	}

	// Resolve e valida o workdir contra a allowlist.
	workdir, err := s.resolveWorkdir(req, log)
	if err != nil {
		writeError(w, http.StatusForbidden, "workdir_forbidden", err.Error())
		return
	}
	req.Workdir = workdir

	// Valida plugin_dirs contra a allowlist (cada um deve estar sob uma raiz).
	for _, dir := range req.PluginDirs {
		if _, err := s.validatePath(dir); err != nil {
			writeError(w, http.StatusForbidden, "plugin_dir_forbidden",
				fmt.Sprintf("plugin_dir fora da allowlist: %s", dir))
			return
		}
	}

	// Adquire slot de concorrência (respeita timeout/cancelamento via ctx).
	ctx, cancel := s.runContext(r)
	defer cancel()
	if err := s.acquire(ctx); err != nil {
		writeError(w, http.StatusTooManyRequests, "busy", "servidor ocupado, tente novamente")
		return
	}
	defer s.release()

	if req.Stream {
		s.streamRun(ctx, w, r, req)
		return
	}

	res, sessionID, runErr := s.runner.Run(ctx, req)
	if sessionID != "" {
		if serr := s.sessions.Upsert(sessionID, req.Workdir, req.Model, ""); serr != nil {
			log.Warn("falha ao persistir sessão", "error", serr)
		}
	}
	if runErr != nil {
		s.writeRunError(w, log, runErr, res, sessionID)
		return
	}
	log.Info("run concluído",
		"session_id", sessionID, "model", req.Model,
		"is_error", res.IsError, "subtype", res.Subtype,
		"num_turns", res.NumTurns, "cost_usd", res.TotalCostUSD)
	writeJSON(w, http.StatusOK, res)
}

// writeRunError mapeia falhas do runner para status HTTP adequados.
func (s *Server) writeRunError(w http.ResponseWriter, log *slog.Logger, runErr error, res runner.Result, sessionID string) {
	var exitErr *runner.ExitError
	switch {
	case errors.Is(runErr, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, "timeout", "execução excedeu o tempo limite")
	case errors.As(runErr, &exitErr):
		log.Error("claude falhou", "code", exitErr.Code, "stderr", truncate(exitErr.Stderr, 500), "session_id", sessionID)
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": errorDetail{
				Code:    "claude_failed",
				Message: fmt.Sprintf("claude saiu com código %d", exitErr.Code),
				Details: map[string]any{"stderr": truncate(exitErr.Stderr, 2000), "session_id": sessionID, "result": res.Result},
			},
		})
	default:
		log.Error("erro de execução", "error", runErr, "session_id", sessionID)
		writeError(w, http.StatusInternalServerError, "run_failed", runErr.Error())
	}
}

// --- sessions ---

func (s *Server) handleListSessions(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"sessions": s.sessions.List()})
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rec, ok := s.sessions.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "sessão desconhecida")
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// --- workdir allowlist ---

// resolveWorkdir determina o workdir efetivo: o fornecido na requisição ou,
// se ausente, herdado de uma sessão em resume. Valida contra a allowlist.
func (s *Server) resolveWorkdir(req runner.RunRequest, log *slog.Logger) (string, error) {
	workdir := req.Workdir
	if workdir == "" && req.Resume != "" {
		if rec, ok := s.sessions.Get(req.Resume); ok {
			workdir = rec.Workdir
			log.Info("workdir herdado da sessão", "session_id", req.Resume, "workdir", workdir)
		}
	}
	if workdir == "" {
		return "", fmt.Errorf("workdir é obrigatório (ou um resume de sessão conhecida)")
	}
	return s.validatePath(workdir)
}

// validatePath resolve symlinks e ".." e exige que o caminho esteja sob uma
// das raízes permitidas. Retorna o caminho absoluto canônico.
func (s *Server) validatePath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("caminho inválido: %w", err)
	}
	clean := canonicalize(abs)
	for _, root := range s.cfg.AllowedRoots {
		rootAbs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		rootAbs = canonicalize(rootAbs)
		if clean == rootAbs || strings.HasPrefix(clean, rootAbs+string(filepath.Separator)) {
			return clean, nil
		}
	}
	return "", fmt.Errorf("workdir %q fora das raízes permitidas", p)
}

// canonicalize resolve symlinks no caminho. Se o caminho não existir, resolve
// o ancestral existente mais profundo e reanexa o restante — assim a
// comparação com a raiz é consistente mesmo para diretórios ainda inexistentes
// (ex.: /var -> /private/var no macOS).
func canonicalize(p string) string {
	p = filepath.Clean(p)
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	dir := p
	var rest []string
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return p // chegou à raiz sem ancestral resolvível
		}
		rest = append([]string{filepath.Base(dir)}, rest...)
		dir = parent
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(append([]string{resolved}, rest...)...)
		}
	}
}

// truncate corta uma string para no máximo n bytes.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
