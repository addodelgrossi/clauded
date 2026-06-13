package server

import (
	"net/http"

	"github.com/addodelgrossi/clauded/web"
)

// handleUI serve a página de chat de demonstração (pública, sem auth).
// A página em si pede o Bearer token ao usuário e o envia em /v1/runs.
func (s *Server) handleUI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(web.IndexHTML)
}
