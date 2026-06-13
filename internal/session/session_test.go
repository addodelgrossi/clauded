package session

import (
	"path/filepath"
	"testing"
)

func TestEncodeProjectDir(t *testing.T) {
	cases := map[string]string{
		"/Users/me/proj": "-Users-me-proj",
		"/tmp/a_b.c":     "-tmp-a-b-c",
		"relativo/path":  "relativo-path",
	}
	for in, want := range cases {
		if got := EncodeProjectDir(in); got != want {
			t.Errorf("EncodeProjectDir(%q) = %q, quer %q", in, got, want)
		}
	}
}

func TestStore_UpsertGetListPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Upsert("id-1", "/tmp/proj", "sonnet", "titulo"); err != nil {
		t.Fatal(err)
	}
	rec, ok := st.Get("id-1")
	if !ok || rec.Workdir != "/tmp/proj" || rec.Model != "sonnet" {
		t.Fatalf("registro inesperado: %+v ok=%v", rec, ok)
	}
	if rec.CreatedAt.IsZero() || rec.LastUsed.IsZero() {
		t.Errorf("timestamps não preenchidos: %+v", rec)
	}

	// Reabre e confirma persistência.
	st2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if rec2, ok := st2.Get("id-1"); !ok || rec2.Workdir != "/tmp/proj" {
		t.Errorf("persistência falhou: %+v ok=%v", rec2, ok)
	}
	if len(st2.List()) != 1 {
		t.Errorf("List() esperava 1, got %d", len(st2.List()))
	}
}

func TestStore_UpsertEmptyIDNoop(t *testing.T) {
	st, _ := Open(filepath.Join(t.TempDir(), "s.json"))
	if err := st.Upsert("", "/tmp", "", ""); err != nil {
		t.Errorf("upsert id vazio deve ser noop, got %v", err)
	}
	if len(st.List()) != 0 {
		t.Errorf("não deve persistir id vazio")
	}
}
