// Package config carrega a configuração do clauded a partir de (em ordem de
// precedência) flags de linha de comando, variáveis de ambiente CLAUDED_*,
// arquivo YAML e, por fim, valores default.
package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config agrega todas as opções de runtime do daemon.
type Config struct {
	// Addr é o endereço de bind do servidor HTTP.
	Addr string `yaml:"addr"`
	// AllowedRoots são as raízes (caminhos absolutos) sob as quais um workdir
	// de requisição é permitido. Qualquer workdir fora delas é rejeitado.
	AllowedRoots []string `yaml:"allowed_roots"`
	// MaxConcurrency limita execuções simultâneas do CLI claude.
	MaxConcurrency int `yaml:"max_concurrency"`
	// DefaultModel é o modelo usado quando a requisição não especifica um.
	DefaultModel string `yaml:"default_model"`
	// ClaudeBin é o caminho (ou nome no PATH) do binário claude.
	ClaudeBin string `yaml:"claude_bin"`
	// RunTimeout é o timeout aplicado a cada execução do claude.
	RunTimeout time.Duration `yaml:"run_timeout"`
	// AllowDangerous habilita os permission modes perigosos
	// (bypassPermissions, dontAsk). Default: false.
	AllowDangerous bool `yaml:"allow_dangerous"`
	// LogFormat é "json" (produção) ou "text" (desenvolvimento).
	LogFormat string `yaml:"log_format"`
	// LogLevel é "debug", "info", "warn" ou "error".
	LogLevel string `yaml:"log_level"`
	// SessionStore é o caminho do arquivo JSON que mapeia session_id -> workdir.
	SessionStore string `yaml:"session_store"`
	// RateLimitPerMinute limita requisições por cliente por minuto (0 = sem limite).
	RateLimitPerMinute int `yaml:"rate_limit_per_minute"`
	// MetricsEnabled expõe /metrics (Prometheus) quando true.
	MetricsEnabled bool `yaml:"metrics_enabled"`

	// APIToken é o Bearer token exigido nas requisições. Vem somente de env
	// (CLAUDED_API_TOKEN); nunca é lido de arquivo nem logado.
	APIToken string `yaml:"-"`
	// OAuthToken é o CLAUDE_CODE_OAUTH_TOKEN da assinatura. Repassado ao
	// subprocesso claude via env; nunca é logado.
	OAuthToken string `yaml:"-"`
	// AnthropicAPIKey, se presente, habilita modo bare. Não é logado.
	AnthropicAPIKey string `yaml:"-"`
}

// Default retorna a configuração com os valores padrão do clauded.
func Default() Config {
	home, _ := os.UserHomeDir()
	return Config{
		Addr:               "127.0.0.1:8787",
		AllowedRoots:       []string{filepath.Join(home, "projects")},
		MaxConcurrency:     2,
		DefaultModel:       "sonnet",
		ClaudeBin:          "claude",
		RunTimeout:         10 * time.Minute,
		AllowDangerous:     false,
		LogFormat:          "json",
		LogLevel:           "info",
		SessionStore:       filepath.Join(home, ".clauded", "sessions.json"),
		RateLimitPerMinute: 60,
		MetricsEnabled:     false,
	}
}

// Load resolve a configuração final aplicando, em ordem crescente de
// precedência: defaults, arquivo YAML, variáveis de ambiente e flags.
// args são os argumentos de linha de comando (normalmente os.Args[1:]).
func Load(args []string) (Config, error) {
	cfg := Default()

	// --- Pré-scan das flags só para descobrir o caminho do arquivo de config,
	// pois o arquivo é aplicado antes das env e flags. ---
	configPath := os.Getenv("CLAUDED_CONFIG")
	pre := flag.NewFlagSet("clauded-pre", flag.ContinueOnError)
	pre.SetOutput(newNullWriter())
	pre.String("config", "", "caminho do arquivo de configuração YAML")
	// Ignora erros e flags desconhecidas no pré-scan.
	_ = pre.Parse(stripUnknownExcept(args, "config"))
	if f := pre.Lookup("config"); f != nil && f.Value.String() != "" {
		configPath = f.Value.String()
	}

	// --- Camada arquivo YAML ---
	if configPath != "" {
		if err := applyYAMLFile(&cfg, configPath); err != nil {
			return cfg, fmt.Errorf("carregando config %q: %w", configPath, err)
		}
	}

	// --- Camada env (CLAUDED_*) ---
	applyEnv(&cfg)

	// --- Camada flags (maior precedência) ---
	if err := applyFlags(&cfg, args); err != nil {
		return cfg, err
	}

	return cfg, nil
}

func applyYAMLFile(cfg *Config, path string) error {
	data, err := os.ReadFile(path) //nolint:gosec // caminho fornecido pelo operador
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, cfg)
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("CLAUDED_ADDR"); v != "" {
		cfg.Addr = v
	}
	if v := os.Getenv("CLAUDED_ALLOWED_ROOTS"); v != "" {
		cfg.AllowedRoots = splitList(v)
	}
	if v := os.Getenv("CLAUDED_MAX_CONCURRENCY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.MaxConcurrency = n
		}
	}
	if v := os.Getenv("CLAUDED_DEFAULT_MODEL"); v != "" {
		cfg.DefaultModel = v
	}
	if v := os.Getenv("CLAUDED_CLAUDE_BIN"); v != "" {
		cfg.ClaudeBin = v
	}
	if v := os.Getenv("CLAUDED_RUN_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.RunTimeout = d
		}
	}
	if v := os.Getenv("CLAUDED_ALLOW_DANGEROUS"); v != "" {
		cfg.AllowDangerous = isTruthy(v)
	}
	if v := os.Getenv("CLAUDED_LOG_FORMAT"); v != "" {
		cfg.LogFormat = v
	}
	if v := os.Getenv("CLAUDED_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
	if v := os.Getenv("CLAUDED_SESSION_STORE"); v != "" {
		cfg.SessionStore = v
	}
	if v := os.Getenv("CLAUDED_RATE_LIMIT_PER_MINUTE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.RateLimitPerMinute = n
		}
	}
	if v := os.Getenv("CLAUDED_METRICS_ENABLED"); v != "" {
		cfg.MetricsEnabled = isTruthy(v)
	}

	// Segredos: somente env, nunca arquivo.
	cfg.APIToken = os.Getenv("CLAUDED_API_TOKEN")
	cfg.OAuthToken = os.Getenv("CLAUDE_CODE_OAUTH_TOKEN")
	cfg.AnthropicAPIKey = os.Getenv("ANTHROPIC_API_KEY")
}

func applyFlags(cfg *Config, args []string) error {
	fs := flag.NewFlagSet("clauded", flag.ContinueOnError)
	fs.String("config", "", "caminho do arquivo de configuração YAML")

	addr := fs.String("addr", cfg.Addr, "endereço de bind do servidor HTTP")
	roots := fs.String("allowed-roots", strings.Join(cfg.AllowedRoots, string(os.PathListSeparator)),
		"raízes permitidas para workdir, separadas por "+string(os.PathListSeparator))
	maxConc := fs.Int("max-concurrency", cfg.MaxConcurrency, "execuções simultâneas do claude")
	model := fs.String("default-model", cfg.DefaultModel, "modelo padrão")
	bin := fs.String("claude-bin", cfg.ClaudeBin, "caminho do binário claude")
	timeout := fs.Duration("run-timeout", cfg.RunTimeout, "timeout por execução")
	logFormat := fs.String("log-format", cfg.LogFormat, "formato de log: json|text")
	logLevel := fs.String("log-level", cfg.LogLevel, "nível de log: debug|info|warn|error")
	sessionStore := fs.String("session-store", cfg.SessionStore, "arquivo do store de sessões")
	rate := fs.Int("rate-limit-per-minute", cfg.RateLimitPerMinute, "requisições por cliente por minuto (0=ilimitado)")
	metrics := fs.Bool("metrics", cfg.MetricsEnabled, "habilita endpoint /metrics")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Aplica apenas as flags efetivamente fornecidas (preserva env/arquivo).
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	if set["addr"] {
		cfg.Addr = *addr
	}
	if set["allowed-roots"] {
		cfg.AllowedRoots = splitList(*roots)
	}
	if set["max-concurrency"] {
		cfg.MaxConcurrency = *maxConc
	}
	if set["default-model"] {
		cfg.DefaultModel = *model
	}
	if set["claude-bin"] {
		cfg.ClaudeBin = *bin
	}
	if set["run-timeout"] {
		cfg.RunTimeout = *timeout
	}
	if set["log-format"] {
		cfg.LogFormat = *logFormat
	}
	if set["log-level"] {
		cfg.LogLevel = *logLevel
	}
	if set["session-store"] {
		cfg.SessionStore = *sessionStore
	}
	if set["rate-limit-per-minute"] {
		cfg.RateLimitPerMinute = *rate
	}
	if set["metrics"] {
		cfg.MetricsEnabled = *metrics
	}

	return nil
}

// Validate garante que a configuração mínima necessária está presente.
func (c Config) Validate() error {
	var missing []string
	if c.APIToken == "" {
		missing = append(missing, "CLAUDED_API_TOKEN")
	}
	if c.OAuthToken == "" && c.AnthropicAPIKey == "" {
		missing = append(missing, "CLAUDE_CODE_OAUTH_TOKEN (ou ANTHROPIC_API_KEY)")
	}
	if len(missing) > 0 {
		return fmt.Errorf("configuração obrigatória ausente: %s", strings.Join(missing, ", "))
	}
	if c.MaxConcurrency < 1 {
		return fmt.Errorf("max_concurrency deve ser >= 1, recebido %d", c.MaxConcurrency)
	}
	if len(c.AllowedRoots) == 0 {
		return fmt.Errorf("allowed_roots não pode ser vazio")
	}
	switch c.LogFormat {
	case "json", "text":
	default:
		return fmt.Errorf("log_format inválido %q (use json|text)", c.LogFormat)
	}
	return nil
}

// --- helpers ---

func splitList(v string) []string {
	// Aceita separação por PathListSeparator (':'/';') ou por vírgula.
	sep := ","
	if strings.ContainsRune(v, os.PathListSeparator) {
		sep = string(os.PathListSeparator)
	}
	parts := strings.Split(v, sep)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func isTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// stripUnknownExcept reduz args ao subconjunto da flag desejada, para o
// pré-scan do --config tolerar flags ainda não declaradas.
func stripUnknownExcept(args []string, name string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		trimmed := strings.TrimLeft(a, "-")
		switch {
		case trimmed == name:
			out = append(out, a)
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				out = append(out, args[i+1])
				i++
			}
		case strings.HasPrefix(trimmed, name+"="):
			out = append(out, a)
		}
	}
	return out
}

type nullWriter struct{}

func (nullWriter) Write(p []byte) (int, error) { return len(p), nil }

func newNullWriter() *nullWriter { return &nullWriter{} }
