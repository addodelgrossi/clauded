package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// CommandSpec descreve uma invocação concreta do binário claude.
type CommandSpec struct {
	Bin  string
	Args []string
	Env  []string
	Dir  string
}

// Executor abstrai a execução do processo claude, permitindo um fake nos
// testes (sem depender do binário real).
type Executor interface {
	// Run executa o comando até o fim e devolve o stdout completo.
	Run(ctx context.Context, spec CommandSpec) (stdout []byte, err error)
	// Stream inicia o comando e devolve um reader de stdout em tempo real.
	// O chamador deve consumir o reader e então chamar wait() para obter o
	// status de saída e liberar recursos.
	Stream(ctx context.Context, spec CommandSpec) (stdout io.ReadCloser, wait func() error, err error)
}

// ExitError encapsula uma falha do subprocesso claude, preservando o código
// de saída e o stderr para diagnóstico.
type ExitError struct {
	Code   int
	Stderr string
	Err    error
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("claude saiu com código %d: %v", e.Code, e.Err)
}

func (e *ExitError) Unwrap() error { return e.Err }

// ExecExecutor é a implementação real baseada em os/exec.
type ExecExecutor struct{}

// Run implementa Executor.Run.
func (ExecExecutor) Run(ctx context.Context, spec CommandSpec) ([]byte, error) {
	cmd := exec.CommandContext(ctx, spec.Bin, spec.Args...) //nolint:gosec // argv direto, sem shell
	cmd.Dir = spec.Dir
	cmd.Env = spec.Env

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// Garante que filhos sejam mortos junto com o processo no cancelamento.
	cmd.Cancel = func() error { return cmd.Process.Kill() }

	err := cmd.Run()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return stdout.Bytes(), &ExitError{Code: ee.ExitCode(), Stderr: stderr.String(), Err: err}
		}
		return stdout.Bytes(), fmt.Errorf("executando claude: %w", err)
	}
	return stdout.Bytes(), nil
}

// Stream implementa Executor.Stream.
func (ExecExecutor) Stream(ctx context.Context, spec CommandSpec) (io.ReadCloser, func() error, error) {
	cmd := exec.CommandContext(ctx, spec.Bin, spec.Args...) //nolint:gosec // argv direto, sem shell
	cmd.Dir = spec.Dir
	cmd.Env = spec.Env
	cmd.Cancel = func() error { return cmd.Process.Kill() }

	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr

	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("abrindo stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("iniciando claude: %w", err)
	}

	wait := func() error {
		if err := cmd.Wait(); err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				return &ExitError{Code: ee.ExitCode(), Stderr: stderr.String(), Err: err}
			}
			return fmt.Errorf("aguardando claude: %w", err)
		}
		return nil
	}
	return pipe, wait, nil
}

// Runner orquestra a tradução de RunRequest em CommandSpec e delega ao
// Executor. Mantém a configuração estável entre requisições.
type Runner struct {
	bin        string
	oauthToken string
	apiKey     string
	defaults   Defaults
	exec       Executor
}

// Option configura o Runner (functional options).
type Option func(*Runner)

// WithExecutor injeta um Executor alternativo (ex.: fake nos testes).
func WithExecutor(e Executor) Option { return func(r *Runner) { r.exec = e } }

// WithDefaults define os valores default aplicados às requisições.
func WithDefaults(d Defaults) Option { return func(r *Runner) { r.defaults = d } }

// New cria um Runner. bin é o caminho do claude; oauthToken e apiKey são os
// segredos repassados ao subprocesso via env (nunca logados).
func New(bin, oauthToken, apiKey string, opts ...Option) *Runner {
	r := &Runner{
		bin:        bin,
		oauthToken: oauthToken,
		apiKey:     apiKey,
		exec:       ExecExecutor{},
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

// env monta o ambiente do subprocesso a partir do ambiente atual mais os
// segredos de autenticação. CLAUDE_CODE_OAUTH_TOKEN usa a assinatura.
func (r *Runner) env() []string {
	env := os.Environ()
	if r.oauthToken != "" {
		env = append(env, "CLAUDE_CODE_OAUTH_TOKEN="+r.oauthToken)
	}
	if r.apiKey != "" {
		env = append(env, "ANTHROPIC_API_KEY="+r.apiKey)
	}
	return env
}

func (r *Runner) spec(req RunRequest) (CommandSpec, string, error) {
	args, sessionID, err := BuildArgs(req, r.defaults)
	if err != nil {
		return CommandSpec{}, "", err
	}
	return CommandSpec{
		Bin:  r.bin,
		Args: args,
		Env:  r.env(),
		Dir:  req.Workdir,
	}, sessionID, nil
}

// Run executa uma requisição não-streaming e devolve o Result normalizado.
// O sessionID retornado é o efetivo (gerado/fornecido/da resposta do claude).
func (r *Runner) Run(ctx context.Context, req RunRequest) (Result, string, error) {
	spec, sessionID, err := r.spec(req)
	if err != nil {
		return Result{}, "", err
	}
	stdout, runErr := r.exec.Run(ctx, spec)
	if runErr != nil {
		// Mesmo com erro de saída, o claude pode ter emitido um JSON de
		// resultado útil (ex.: is_error com mensagem). Tenta parsear.
		if res, perr := ParseResult(stdout); perr == nil && res.SessionID != "" {
			if sessionID == "" {
				sessionID = res.SessionID
			}
			return res, sessionID, runErr
		}
		return Result{}, sessionID, runErr
	}
	res, err := ParseResult(stdout)
	if err != nil {
		return Result{}, sessionID, err
	}
	if sessionID == "" {
		sessionID = res.SessionID
	}
	return res, sessionID, nil
}

// Stream executa uma requisição em modo streaming, devolvendo um reader do
// stdout (stream-json, uma linha JSON por evento) e uma função wait().
func (r *Runner) Stream(ctx context.Context, req RunRequest) (io.ReadCloser, func() error, string, error) {
	req.Stream = true
	spec, sessionID, err := r.spec(req)
	if err != nil {
		return nil, nil, "", err
	}
	pipe, wait, err := r.exec.Stream(ctx, spec)
	if err != nil {
		return nil, nil, sessionID, err
	}
	return pipe, wait, sessionID, nil
}
