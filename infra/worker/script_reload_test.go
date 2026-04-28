package worker

import (
	"os"
	"path/filepath"
	"testing"

	"utobot/domain/graph"
	walletcrypto "utobot/infra/crypto"
	"utobot/infra/network"
)

type stubBrainManager struct {
	loadCalls int
	lastPath  string
	lastJS    []byte
	err       error
}

func (s *stubBrainManager) LoadSource(path string, source []byte) error {
	s.loadCalls++
	s.lastPath = path
	s.lastJS = append([]byte(nil), source...)
	return s.err
}

type stubClient struct {
	heightCalls int
	fetchCalls  int
	height      int
	script      []byte
}

func (s *stubClient) FetchJavascript(chainID string) ([]byte, error) {
	s.fetchCalls++
	return append([]byte(nil), s.script...), nil
}

func (s *stubClient) GetLastBlockIndex(chainID string) (int, error) {
	s.heightCalls++
	return s.height, nil
}

func (s *stubClient) GetLastState(chainID string) (*graph.Graph, error) {
	g := graph.NewGraphWithID(chainID)
	return &g, nil
}

func (s *stubClient) Deploy(req network.DeployRequest, wallet walletcrypto.KeyPair) (network.DeployResult, error) {
	return network.DeployResult{}, nil
}

var _ network.Client = (*stubClient)(nil)

func TestBootstrapPersistsHeight(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "height.json")

	w := New(&stubClient{}, &stubBrainManager{}, "chain-1", statePath, 0)
	if err := w.Bootstrap(42); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	if string(data) != `{"height":42}` {
		t.Fatalf("unexpected state file content: %s", string(data))
	}
}

func TestSyncOnceDoesNotReloadWhenStateMissing(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "height.json")

	client := &stubClient{
		height: 42,
		script: []byte("script-v1"),
	}
	manager := &stubBrainManager{}
	w := New(client, manager, "chain-1", statePath, 0)

	reloaded, err := w.SyncOnce()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if reloaded {
		t.Fatal("expected no reload when state is missing")
	}
	if client.fetchCalls != 0 {
		t.Fatalf("expected no script fetch, got %d", client.fetchCalls)
	}
	if manager.loadCalls != 0 {
		t.Fatalf("expected no reload, got %d", manager.loadCalls)
	}

	if _, err := os.ReadFile(statePath); !os.IsNotExist(err) {
		t.Fatalf("expected state file to remain absent, got err=%v", err)
	}
}

func TestSyncOnceReloadsWhenHeightChanges(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "height.json")

	if err := os.WriteFile(statePath, []byte(`{"height":42}`), 0o600); err != nil {
		t.Fatalf("seed state file: %v", err)
	}

	client := &stubClient{
		height: 43,
		script: []byte("script-v2"),
	}
	manager := &stubBrainManager{}
	w := New(client, manager, "chain-1", statePath, 0)

	reloaded, err := w.SyncOnce()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !reloaded {
		t.Fatal("expected reload when height changes")
	}
	if client.fetchCalls != 1 {
		t.Fatalf("expected one script fetch, got %d", client.fetchCalls)
	}
	if manager.loadCalls != 1 {
		t.Fatalf("expected one reload, got %d", manager.loadCalls)
	}
	if string(manager.lastJS) != "script-v2" {
		t.Fatalf("unexpected loaded script: %s", string(manager.lastJS))
	}

	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	if string(data) != `{"height":43}` {
		t.Fatalf("unexpected state file content: %s", string(data))
	}
}

func TestSyncOnceSkipsReloadWhenHeightUnchanged(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "height.json")

	if err := os.WriteFile(statePath, []byte(`{"height":42}`), 0o600); err != nil {
		t.Fatalf("seed state file: %v", err)
	}

	client := &stubClient{
		height: 42,
		script: []byte("script-v2"),
	}
	manager := &stubBrainManager{}
	w := New(client, manager, "chain-1", statePath, 0)

	reloaded, err := w.SyncOnce()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if reloaded {
		t.Fatal("expected no reload when height is unchanged")
	}
	if client.fetchCalls != 0 {
		t.Fatalf("expected no script fetch, got %d", client.fetchCalls)
	}
	if manager.loadCalls != 0 {
		t.Fatalf("expected no reload, got %d", manager.loadCalls)
	}
}
