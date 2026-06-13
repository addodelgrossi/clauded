// Package session mantém o mapeamento session_id -> workdir num arquivo JSON.
//
// Retomar uma sessão do Claude Code (resume/continue) só funciona se o
// processo for reexecutado com o MESMO cwd de quando a sessão foi criada,
// pois o histórico vive em ~/.claude/projects/<cwd-codificado>/<id>.jsonl.
// Este store amarra cada session_id ao seu workdir para garantir isso.
package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Record é o metadado persistido de uma sessão.
type Record struct {
	ID        string    `json:"id"`
	Workdir   string    `json:"workdir"`
	Model     string    `json:"model,omitempty"`
	Title     string    `json:"title,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	LastUsed  time.Time `json:"last_used"`
}

// Store é um mapa session_id -> Record persistido em arquivo JSON, seguro
// para uso concorrente.
type Store struct {
	path string
	mu   sync.RWMutex
	recs map[string]Record
	now  func() time.Time
}

// Open carrega (ou inicializa) o store no caminho indicado.
func Open(path string) (*Store, error) {
	s := &Store{
		path: path,
		recs: map[string]Record{},
		now:  time.Now,
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("criando diretório do store: %w", err)
	}
	data, err := os.ReadFile(path) //nolint:gosec // caminho de config do operador
	switch {
	case os.IsNotExist(err):
		return s, nil
	case err != nil:
		return nil, fmt.Errorf("lendo store de sessões: %w", err)
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &s.recs); err != nil {
			return nil, fmt.Errorf("parse do store de sessões: %w", err)
		}
	}
	return s, nil
}

// Get retorna o registro de uma sessão e se ela existe.
func (s *Store) Get(id string) (Record, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.recs[id]
	return r, ok
}

// List devolve todos os registros ordenados por LastUsed decrescente.
func (s *Store) List() []Record {
	s.mu.RLock()
	out := make([]Record, 0, len(s.recs))
	for _, r := range s.recs {
		out = append(out, r)
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].LastUsed.After(out[j].LastUsed) })
	return out
}

// Upsert cria ou atualiza o registro de uma sessão e persiste o store.
// Atualiza LastUsed; define CreatedAt na primeira gravação.
func (s *Store) Upsert(id, workdir, model, title string) error {
	if id == "" {
		return nil // nada a persistir (ex.: continue sem id conhecido)
	}
	s.mu.Lock()
	now := s.now()
	rec, ok := s.recs[id]
	if !ok {
		rec = Record{ID: id, CreatedAt: now}
	}
	if workdir != "" {
		rec.Workdir = workdir
	}
	if model != "" {
		rec.Model = model
	}
	if title != "" {
		rec.Title = title
	}
	rec.LastUsed = now
	s.recs[id] = rec
	snapshot := s.snapshotLocked()
	s.mu.Unlock()
	return s.persist(snapshot)
}

func (s *Store) snapshotLocked() map[string]Record {
	cp := make(map[string]Record, len(s.recs))
	for k, v := range s.recs {
		cp[k] = v
	}
	return cp
}

// persist grava o store de forma atômica (write-temp + rename).
func (s *Store) persist(recs map[string]Record) error {
	data, err := json.MarshalIndent(recs, "", "  ")
	if err != nil {
		return fmt.Errorf("serializando store: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("gravando store temporário: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("renomeando store: %w", err)
	}
	return nil
}

// EncodeProjectDir replica o esquema do Claude Code para o nome do diretório
// de projeto: o caminho absoluto do cwd com cada caractere não-alfanumérico
// substituído por '-'. Ex.: /Users/me/proj -> -Users-me-proj.
func EncodeProjectDir(cwd string) string {
	var b strings.Builder
	b.Grow(len(cwd))
	for _, r := range cwd {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

// DiscoverFromDisk lê o diretório ~/.claude/projects (ou configRoot/projects)
// e retorna os IDs de sessão (.jsonl) encontrados para um dado workdir.
// Útil para complementar GET /v1/sessions com sessões criadas fora do store.
func DiscoverFromDisk(configRoot, workdir string) ([]string, error) {
	dir := filepath.Join(configRoot, "projects", EncodeProjectDir(workdir))
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lendo diretório de projetos: %w", err)
	}
	var ids []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(name, ".jsonl"))
	}
	return ids, nil
}

// ClaudeConfigRoot devolve a raiz de configuração do Claude Code, respeitando
// CLAUDE_CONFIG_DIR e caindo para ~/.claude.
func ClaudeConfigRoot() string {
	if v := os.Getenv("CLAUDE_CONFIG_DIR"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}
