package runner

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"github.com/google/uuid"
)

// RunRequest é o corpo JSON aceito por POST /v1/runs. Cada campo mapeia para
// uma ou mais flags do CLI claude em modo print (-p). Ver README §"Referência
// da API" para a tabela completa.
type RunRequest struct {
	Prompt   string `json:"prompt"`
	Workdir  string `json:"workdir,omitempty"`
	Model    string `json:"model,omitempty"`
	Continue bool   `json:"continue,omitempty"`

	SessionID string `json:"session_id,omitempty"`
	Resume    string `json:"resume,omitempty"`
	Fork      bool   `json:"fork,omitempty"`

	OutputFormat string `json:"output_format,omitempty"`
	Stream       bool   `json:"stream,omitempty"`

	PermissionMode string `json:"permission_mode,omitempty"`
	Tools          string `json:"tools,omitempty"`

	MaxTurns     int     `json:"max_turns,omitempty"`
	MaxBudgetUSD float64 `json:"max_budget_usd,omitempty"`

	AppendSystemPrompt string `json:"append_system_prompt,omitempty"`
	SystemPrompt       string `json:"system_prompt,omitempty"`

	MCPConfig       json.RawMessage `json:"mcp_config,omitempty"`
	StrictMCPConfig bool            `json:"strict_mcp_config,omitempty"`
	Agents          json.RawMessage `json:"agents,omitempty"`
	JSONSchema      json.RawMessage `json:"json_schema,omitempty"`

	Bare           bool     `json:"bare,omitempty"`
	Effort         string   `json:"effort,omitempty"`
	FallbackModel  string   `json:"fallback_model,omitempty"`
	SettingSources string   `json:"setting_sources,omitempty"`
	PluginDirs     []string `json:"plugin_dirs,omitempty"`
	PluginURLs     []string `json:"plugin_urls,omitempty"`
}

// Defaults fornece valores aplicados quando a requisição os omite.
type Defaults struct {
	Model string
}

// Conjuntos de valores válidos para enums, validados no servidor antes de
// invocar o CLI (rejeição com 400 em vez de erro opaco do subprocesso).
var (
	validOutputFormats  = map[string]bool{"text": true, "json": true, "stream-json": true}
	validPermissionMode = map[string]bool{
		"default": true, "acceptEdits": true, "plan": true,
		"auto": true, "dontAsk": true, "bypassPermissions": true,
	}
	validEffort = map[string]bool{"low": true, "medium": true, "high": true, "xhigh": true}

	// DangerousPermissionModes exigem CLAUDED_ALLOW_DANGEROUS=true.
	DangerousPermissionModes = map[string]bool{"bypassPermissions": true, "dontAsk": true}
)

// Validate verifica os campos da requisição independentemente de política de
// segurança (que é aplicada no handler). Retorna erro em valores inválidos.
func (r RunRequest) Validate() error {
	if r.Prompt == "" {
		return fmt.Errorf("campo obrigatório ausente: prompt")
	}
	if r.OutputFormat != "" && !validOutputFormats[r.OutputFormat] {
		return fmt.Errorf("output_format inválido %q (use text|json|stream-json)", r.OutputFormat)
	}
	if r.PermissionMode != "" && !validPermissionMode[r.PermissionMode] {
		return fmt.Errorf("permission_mode inválido %q", r.PermissionMode)
	}
	if r.Effort != "" && !validEffort[r.Effort] {
		return fmt.Errorf("effort inválido %q (use low|medium|high|xhigh)", r.Effort)
	}
	if r.MaxTurns < 0 {
		return fmt.Errorf("max_turns não pode ser negativo")
	}
	if r.MaxBudgetUSD < 0 {
		return fmt.Errorf("max_budget_usd não pode ser negativo")
	}
	if r.SessionID != "" {
		if _, err := uuid.Parse(r.SessionID); err != nil {
			return fmt.Errorf("session_id deve ser um UUID válido: %w", err)
		}
	}
	if r.Resume != "" {
		if _, err := uuid.Parse(r.Resume); err != nil {
			return fmt.Errorf("resume deve ser um UUID válido: %w", err)
		}
	}
	if r.Continue && r.Resume != "" {
		return fmt.Errorf("continue e resume são mutuamente exclusivos")
	}
	if r.JSONSchema != nil && !json.Valid(r.JSONSchema) {
		return fmt.Errorf("json_schema não é um JSON válido")
	}
	if r.MCPConfig != nil && !json.Valid(r.MCPConfig) {
		return fmt.Errorf("mcp_config não é um JSON válido")
	}
	if r.Agents != nil && !json.Valid(r.Agents) {
		return fmt.Errorf("agents não é um JSON válido")
	}
	return nil
}

// EffectiveOutputFormat resolve o output-format real considerando stream.
func (r RunRequest) EffectiveOutputFormat() string {
	if r.Stream {
		return "stream-json"
	}
	if r.OutputFormat != "" {
		return r.OutputFormat
	}
	return "json"
}

// BuildArgs traduz uma RunRequest validada para o argv do CLI claude.
// É uma função pura (sem efeitos colaterais além de gerar um UUID quando
// session_id está ausente) — o núcleo testável do runner.
//
// Retorna também o session_id efetivo (gerado ou fornecido) para que o
// chamador possa persistir o mapeamento session_id -> workdir.
func BuildArgs(r RunRequest, d Defaults) (args []string, sessionID string, err error) {
	if err := r.Validate(); err != nil {
		return nil, "", err
	}

	// Sempre modo print não-interativo.
	args = append(args, "-p", r.Prompt)

	// Output format (stream força stream-json).
	args = append(args, "--output-format", r.EffectiveOutputFormat())
	// Em stream-json, o claude exige --verbose para emitir os eventos.
	if r.EffectiveOutputFormat() == "stream-json" {
		args = append(args, "--verbose")
	}

	// Modelo (default da config quando ausente).
	model := r.Model
	if model == "" {
		model = d.Model
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if r.FallbackModel != "" {
		args = append(args, "--fallback-model", r.FallbackModel)
	}

	// Sessão: resume/continue são mutuamente exclusivos com a fixação de
	// session-id novo. A precedência é resume > continue > session-id.
	switch {
	case r.Resume != "":
		args = append(args, "--resume", r.Resume)
		sessionID = r.Resume
	case r.Continue:
		args = append(args, "--continue")
		// session-id desconhecido até o claude responder.
	default:
		sessionID = r.SessionID
		if sessionID == "" {
			sessionID = uuid.NewString()
		}
		args = append(args, "--session-id", sessionID)
	}
	if r.Fork {
		args = append(args, "--fork-session")
	}

	// Diretório de trabalho adicional (o cwd do processo também é setado pelo
	// runner; --add-dir garante acesso explícito de ferramentas).
	if r.Workdir != "" {
		args = append(args, "--add-dir", r.Workdir)
	}

	if r.PermissionMode != "" {
		args = append(args, "--permission-mode", r.PermissionMode)
	}
	if r.Tools != "" {
		args = append(args, "--tools", r.Tools)
	}

	if r.MaxTurns > 0 {
		args = append(args, "--max-turns", strconv.Itoa(r.MaxTurns))
	}
	if r.MaxBudgetUSD > 0 {
		args = append(args, "--max-budget-usd", strconv.FormatFloat(r.MaxBudgetUSD, 'f', -1, 64))
	}

	if r.SystemPrompt != "" {
		args = append(args, "--system-prompt", r.SystemPrompt)
	}
	if r.AppendSystemPrompt != "" {
		args = append(args, "--append-system-prompt", r.AppendSystemPrompt)
	}

	if len(r.MCPConfig) > 0 {
		args = append(args, "--mcp-config", string(r.MCPConfig))
	}
	if r.StrictMCPConfig {
		args = append(args, "--strict-mcp-config")
	}
	if len(r.Agents) > 0 {
		args = append(args, "--agents", string(r.Agents))
	}
	if len(r.JSONSchema) > 0 {
		args = append(args, "--json-schema", string(r.JSONSchema))
	}

	if r.Bare {
		args = append(args, "--bare")
	}
	if r.Effort != "" {
		args = append(args, "--effort", r.Effort)
	}
	if r.SettingSources != "" {
		args = append(args, "--setting-sources", r.SettingSources)
	}
	for _, dir := range r.PluginDirs {
		args = append(args, "--plugin-dir", dir)
	}
	for _, u := range r.PluginURLs {
		args = append(args, "--plugin-url", u)
	}

	return args, sessionID, nil
}

// SortedFlagNames é um utilitário de teste/depuração que extrai os nomes de
// flag (tokens iniciados por "--") presentes em args, ordenados.
func SortedFlagNames(args []string) []string {
	var names []string
	for _, a := range args {
		if len(a) > 2 && a[:2] == "--" {
			names = append(names, a)
		}
	}
	sort.Strings(names)
	return names
}
