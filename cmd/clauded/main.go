// Command clauded é um daemon HTTP que expõe o Claude Code headless (claude -p)
// como uma API REST/streaming, autenticando pela assinatura via
// CLAUDE_CODE_OAUTH_TOKEN.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/addodelgrossi/clauded/internal/config"
	"github.com/addodelgrossi/clauded/internal/server"
	"github.com/addodelgrossi/clauded/internal/session"
	"github.com/addodelgrossi/clauded/internal/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "clauded:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	// Atalho para --version sem exigir config completa.
	for _, a := range args {
		if a == "--version" || a == "-version" {
			info := version.Get()
			fmt.Printf("clauded %s (commit %s, build %s, %s)\n", info.Version, info.Commit, info.Date, info.GoVersion)
			return nil
		}
	}

	cfg, err := config.Load(args)
	if err != nil {
		return err
	}

	logger := newLogger(cfg)
	slog.SetDefault(logger)

	if err := cfg.Validate(); err != nil {
		return err
	}

	store, err := session.Open(cfg.SessionStore)
	if err != nil {
		return fmt.Errorf("abrindo store de sessões: %w", err)
	}

	srv := server.New(cfg,
		server.WithLogger(logger),
		server.WithSessionStore(store),
	)
	httpSrv := srv.NewHTTPServer()

	// Contexto cancelado em SIGINT/SIGTERM para shutdown gracioso.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("clauded iniciado",
			"addr", cfg.Addr,
			"version", version.Version,
			"max_concurrency", cfg.MaxConcurrency,
			"allowed_roots", cfg.AllowedRoots,
		)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("servidor falhou: %w", err)
	case <-ctx.Done():
		logger.Info("sinal recebido, encerrando graciosamente")
	}

	// Drena conexões em andamento com timeout. SSE/runs longos são cancelados
	// pelo cancelamento do context do servidor.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	logger.Info("clauded encerrado")
	return nil
}

func newLogger(cfg config.Config) *slog.Logger {
	level := slog.LevelInfo
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler
	if cfg.LogFormat == "text" {
		h = slog.NewTextHandler(os.Stdout, opts)
	} else {
		h = slog.NewJSONHandler(os.Stdout, opts)
	}
	return slog.New(h)
}
