package state

import (
	"path/filepath"
	"testing"
)

func TestSQLiteStoreSaveLoad(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSQLiteStore(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatalf("create sqlite store: %v", err)
	}
	defer store.Close()

	if err := store.SaveContext([]byte(`{"counter":1,"tags":["one"]}`)); err != nil {
		t.Fatalf("save context: %v", err)
	}

	data, ok, err := store.LoadContext()
	if err != nil {
		t.Fatalf("load context: %v", err)
	}
	if !ok {
		t.Fatal("expected context to exist")
	}
	if string(data) != `{"counter":1,"tags":["one"]}` {
		t.Fatalf("unexpected context payload: %s", string(data))
	}
}
