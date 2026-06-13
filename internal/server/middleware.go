package server

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

type ctxKey string

const (
	ctxKeyRunID  ctxKey = "run_id"
	ctxKeyClient ctxKey = "client"
)

// statusRecorder captura o status HTTP e bytes escritos para o log.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// Unwrap permite que http.ResponseController acesse o ResponseWriter original
// (necessário para flush em SSE).
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// auth exige Authorization: Bearer <token> comparado em tempo constante.
func (s *Server) auth(next http.Handler) http.Handler {
	want := []byte(s.cfg.APIToken)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := bearerToken(r)
		if got == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Authorization Bearer ausente")
			return
		}
		if subtle.ConstantTimeCompare([]byte(got), want) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized", "token inválido")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return h[len(prefix):]
	}
	return ""
}

// loggingMW registra cada requisição com um run_id estável e métricas básicas.
// Tokens e prompt completo nunca são logados em nível info.
func (s *Server) loggingMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runID := uuid.NewString()
		ctx := context.WithValue(r.Context(), ctxKeyRunID, runID)
		ctx = context.WithValue(ctx, ctxKeyClient, clientKey(r))
		rec := &statusRecorder{ResponseWriter: w}
		start := s.now()

		next.ServeHTTP(rec, r.WithContext(ctx))

		s.log.Info("request",
			"run_id", runID,
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"bytes", rec.bytes,
			"duration_ms", s.now().Sub(start).Milliseconds(),
			"client", clientKey(r),
		)
	})
}

// recoverMW protege contra panics em handlers, devolvendo 500.
func (s *Server) recoverMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("panic recuperado", "error", rec, "path", r.URL.Path)
				writeError(w, http.StatusInternalServerError, "internal_error", "erro interno")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// rateLimitMW aplica token bucket por cliente (token/IP). Isenta /healthz.
func (s *Server) rateLimitMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		if lim := s.limiters.get(clientKey(r)); lim != nil && !lim.Allow() {
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusTooManyRequests, "rate_limited", "limite de requisições excedido")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientKey identifica o cliente para rate limiting: prefixo do token se
// presente, senão o IP remoto. Nunca expõe o token inteiro.
func clientKey(r *http.Request) string {
	if t := bearerToken(r); t != "" {
		if len(t) > 8 {
			return "tok:" + t[:8]
		}
		return "tok:" + t
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return "ip:" + host
}

// logWith devolve um logger anotado com o run_id da requisição.
func (s *Server) logWith(r *http.Request) *slog.Logger {
	if id, ok := r.Context().Value(ctxKeyRunID).(string); ok {
		return s.log.With("run_id", id)
	}
	return s.log
}
