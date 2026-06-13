package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/addodelgrossi/clauded/internal/runner"
)

// runContext deriva um context com o timeout de execução configurado, ligado
// ao context da requisição (cancela se o cliente desconectar).
func (s *Server) runContext(r *http.Request) (context.Context, context.CancelFunc) {
	if s.cfg.RunTimeout > 0 {
		return context.WithTimeout(r.Context(), s.cfg.RunTimeout)
	}
	return context.WithCancel(r.Context())
}

// sseEvent é a forma de cada evento enviado ao cliente.
//
//	event: <name>\n
//	data: <json>\n\n
func writeSSE(w http.ResponseWriter, rc *http.ResponseController, event string, data []byte) error {
	if event != "" {
		if _, err := w.Write([]byte("event: " + event + "\n")); err != nil {
			return err
		}
	}
	if _, err := w.Write([]byte("data: ")); err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	if _, err := w.Write([]byte("\n\n")); err != nil {
		return err
	}
	return rc.Flush()
}

// streamRun executa a requisição em modo stream-json e repassa cada linha do
// stdout do claude como um evento SSE. Encerra com `event: done`.
func (s *Server) streamRun(ctx context.Context, w http.ResponseWriter, r *http.Request, req runner.RunRequest) {
	log := s.logWith(r)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	rc := http.NewResponseController(w)

	pipe, wait, sessionID, err := s.runner.Stream(ctx, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	defer func() { _ = pipe.Close() }()

	w.WriteHeader(http.StatusOK)

	// Reader linha-a-linha; cada linha do stream-json é um objeto JSON.
	// Aumenta o buffer para acomodar mensagens grandes.
	sc := bufio.NewScanner(pipe)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		// Captura o session_id do evento init/system, se ainda não conhecido.
		if sessionID == "" {
			if id := extractSessionID(line); id != "" {
				sessionID = id
			}
		}
		buf := make([]byte, len(line))
		copy(buf, line)
		if err := writeSSE(w, rc, "message", buf); err != nil {
			log.Warn("cliente SSE desconectou", "error", err)
			return
		}
	}

	waitErr := wait()
	scanErr := sc.Err()

	// Persiste a sessão (mesmo em erro de limite, para permitir resume).
	if sessionID != "" {
		if serr := s.sessions.Upsert(sessionID, req.Workdir, req.Model, ""); serr != nil {
			log.Warn("falha ao persistir sessão", "error", serr)
		}
	}

	switch {
	case scanErr != nil:
		errData, _ := json.Marshal(map[string]string{"error": scanErr.Error(), "session_id": sessionID})
		_ = writeSSE(w, rc, "error", errData)
	case waitErr != nil:
		var exitErr *runner.ExitError
		msg := waitErr.Error()
		if errors.As(waitErr, &exitErr) {
			msg = exitErr.Stderr
		}
		errData, _ := json.Marshal(map[string]string{"error": truncate(msg, 2000), "session_id": sessionID})
		_ = writeSSE(w, rc, "error", errData)
	}

	doneData, _ := json.Marshal(map[string]string{"session_id": sessionID})
	_ = writeSSE(w, rc, "done", doneData)
	log.Info("stream concluído", "session_id", sessionID)
}

// extractSessionID tenta ler o campo session_id de uma linha de evento.
func extractSessionID(line []byte) string {
	var probe struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(line, &probe); err == nil {
		return probe.SessionID
	}
	return ""
}
