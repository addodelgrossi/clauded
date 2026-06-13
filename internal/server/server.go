// Package server implementa o daemon HTTP do clauded: roteamento, middlewares
// (auth, logging, recover, rate limit), e os handlers de /v1/runs e sessões.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"golang.org/x/sync/semaphore"
	"golang.org/x/time/rate"

	"github.com/addodelgrossi/clauded/internal/config"
	"github.com/addodelgrossi/clauded/internal/runner"
	"github.com/addodelgrossi/clauded/internal/session"
)

// Server agrega as dependências do daemon. Construído via New.
type Server struct {
	cfg      config.Config
	log      *slog.Logger
	runner   *runner.Runner
	sessions *session.Store

	sem      *semaphore.Weighted
	limiters *clientLimiters

	now func() time.Time
}

// Option configura o Server (functional options).
type Option func(*Server)

// WithLogger injeta o logger estruturado.
func WithLogger(l *slog.Logger) Option { return func(s *Server) { s.log = l } }

// WithRunner injeta o runner (permite fake em testes).
func WithRunner(r *runner.Runner) Option { return func(s *Server) { s.runner = r } }

// WithSessionStore injeta o store de sessões.
func WithSessionStore(st *session.Store) Option { return func(s *Server) { s.sessions = st } }

// WithClock injeta uma fonte de tempo (para testes determinísticos).
func WithClock(now func() time.Time) Option { return func(s *Server) { s.now = now } }

// New constrói o Server a partir da configuração e das opções.
func New(cfg config.Config, opts ...Option) *Server {
	s := &Server{
		cfg: cfg,
		log: slog.Default(),
		now: time.Now,
	}
	for _, o := range opts {
		o(s)
	}
	if s.runner == nil {
		s.runner = runner.New(cfg.ClaudeBin, cfg.OAuthToken, cfg.AnthropicAPIKey,
			runner.WithDefaults(runner.Defaults{Model: cfg.DefaultModel}))
	}
	s.sem = semaphore.NewWeighted(int64(cfg.MaxConcurrency))
	s.limiters = newClientLimiters(cfg.RateLimitPerMinute)
	return s
}

// Handler monta o roteador com middlewares aplicados.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Endpoints públicos (sem auth).
	mux.HandleFunc("GET /healthz", s.handleHealthz)

	// Endpoints autenticados.
	mux.Handle("GET /readyz", s.auth(http.HandlerFunc(s.handleReadyz)))
	mux.Handle("GET /version", s.auth(http.HandlerFunc(s.handleVersion)))
	mux.Handle("POST /v1/runs", s.auth(http.HandlerFunc(s.handleRuns)))
	mux.Handle("GET /v1/sessions", s.auth(http.HandlerFunc(s.handleListSessions)))
	mux.Handle("GET /v1/sessions/{id}", s.auth(http.HandlerFunc(s.handleGetSession)))

	if s.cfg.MetricsEnabled {
		mux.Handle("GET /metrics", s.auth(http.HandlerFunc(s.handleMetrics)))
	}

	// Cadeia global: recover -> logging -> rate limit -> mux.
	return s.recoverMW(s.loggingMW(s.rateLimitMW(mux)))
}

// NewHTTPServer cria um *http.Server configurado com timeouts adequados.
// WriteTimeout é 0 porque SSE exige escrita de longa duração; o controle de
// tempo por execução é feito via context no handler.
func (s *Server) NewHTTPServer() *http.Server {
	return &http.Server{
		Addr:              s.cfg.Addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       120 * time.Second,
	}
}

// clientLimiters mantém um rate.Limiter por chave de cliente (token/IP).
type clientLimiters struct {
	perMinute int
	mu        sync.Mutex
	limiters  map[string]*rate.Limiter
}

func newClientLimiters(perMinute int) *clientLimiters {
	return &clientLimiters{perMinute: perMinute, limiters: map[string]*rate.Limiter{}}
}

func (c *clientLimiters) get(key string) *rate.Limiter {
	if c.perMinute <= 0 {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if l, ok := c.limiters[key]; ok {
		return l
	}
	// burst = perMinute (permite rajada de até 1 minuto de cota).
	lim := rate.NewLimiter(rate.Limit(float64(c.perMinute)/60.0), c.perMinute)
	c.limiters[key] = lim
	return lim
}

// acquire tenta obter um slot de concorrência respeitando o context.
func (s *Server) acquire(ctx context.Context) error {
	if err := s.sem.Acquire(ctx, 1); err != nil {
		return fmt.Errorf("aguardando slot de execução: %w", err)
	}
	return nil
}

func (s *Server) release() { s.sem.Release(1) }
