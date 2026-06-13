package runner

import (
	"encoding/json"
	"fmt"
)

// Result é a forma normalizada da resposta de uma execução não-streaming,
// derivada do objeto final de `--output-format json` do claude.
type Result struct {
	SessionID    string          `json:"session_id"`
	Result       string          `json:"result"`
	IsError      bool            `json:"is_error"`
	Subtype      string          `json:"subtype"`
	NumTurns     int             `json:"num_turns"`
	DurationMS   int64           `json:"duration_ms"`
	TotalCostUSD float64         `json:"total_cost_usd"`
	Usage        json.RawMessage `json:"usage,omitempty"`
}

// claudeJSONResult espelha os campos relevantes do objeto JSON emitido pelo
// claude em --output-format json. Campos extras são ignorados.
type claudeJSONResult struct {
	Type         string          `json:"type"`
	Subtype      string          `json:"subtype"`
	IsError      bool            `json:"is_error"`
	Result       string          `json:"result"`
	SessionID    string          `json:"session_id"`
	NumTurns     int             `json:"num_turns"`
	DurationMS   int64           `json:"duration_ms"`
	TotalCostUSD float64         `json:"total_cost_usd"`
	Usage        json.RawMessage `json:"usage,omitempty"`
}

// ParseResult interpreta o stdout de `--output-format json`.
func ParseResult(stdout []byte) (Result, error) {
	var c claudeJSONResult
	if err := json.Unmarshal(stdout, &c); err != nil {
		return Result{}, fmt.Errorf("parse do output json do claude: %w", err)
	}
	return Result{
		SessionID:    c.SessionID,
		Result:       c.Result,
		IsError:      c.IsError,
		Subtype:      c.Subtype,
		NumTurns:     c.NumTurns,
		DurationMS:   c.DurationMS,
		TotalCostUSD: c.TotalCostUSD,
		Usage:        c.Usage,
	}, nil
}

// LimitReached reporta se o subtype indica parada por limite de turnos ou
// orçamento — caso em que o cliente pode retomar (resume) com limite maior.
func (r Result) LimitReached() bool {
	return r.Subtype == "error_max_turns" || r.Subtype == "error_max_budget_usd"
}
