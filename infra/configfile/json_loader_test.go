package configfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"utobot/domain"
	walletcrypto "utobot/infra/crypto"
)

func TestJSONLoaderLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	content := []byte(`{
		"http_address": "127.0.0.1:8080",
		"port": 8081,
		"wallet_path": "./wallet.json",
		"brain_chain_id": "./brain.js"
	}`)

	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	loader := NewJSONLoader()
	params, err := loader.Load(path)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if _, err := walletcrypto.DecodeEncryptionKey(params.EncryptionKey); err != nil {
		t.Fatalf("expected generated encryption key, got %v", err)
	}

	want := domain.Parameters{
		HTTPAddress:   "127.0.0.1:8080",
		Port:          8081,
		WalletPath:    "./wallet.json",
		BrainChainID:  "./brain.js",
		EncryptionKey: params.EncryptionKey,
	}

	if params != want {
		t.Fatalf("unexpected parameters: got %#v want %#v", params, want)
	}

	fileBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	var persisted map[string]any
	if err := json.Unmarshal(fileBytes, &persisted); err != nil {
		t.Fatalf("decode persisted config: %v", err)
	}
	if persisted["encryption_key"] != params.EncryptionKey {
		t.Fatalf("expected persisted encryption key, got %#v", persisted["encryption_key"])
	}
}

func TestJSONLoaderLoadKeepsExistingEncryptionKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	key, err := walletcrypto.GenerateEncryptionKey()
	if err != nil {
		t.Fatalf("GenerateEncryptionKey() error = %v", err)
	}
	encodedKey, err := walletcrypto.EncodeEncryptionKey(key)
	if err != nil {
		t.Fatalf("EncodeEncryptionKey() error = %v", err)
	}

	content := []byte(`{
		"http_address": "127.0.0.1:8080",
		"port": 8081,
		"wallet_path": "./wallet.json",
		"brain_chain_id": "./brain.js",
		"encryption_key": "` + encodedKey + `"
	}`)

	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	loader := NewJSONLoader()
	params, err := loader.Load(path)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if params.EncryptionKey != encodedKey {
		t.Fatalf("unexpected encryption key: got %q want %q", params.EncryptionKey, encodedKey)
	}
}

func TestJSONLoaderLoadRejectsInvalidEncryptionKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	content := []byte(`{
		"http_address": "127.0.0.1:8080",
		"port": 8081,
		"wallet_path": "./wallet.json",
		"brain_chain_id": "./brain.js",
		"encryption_key": "AA=="
	}`)

	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	loader := NewJSONLoader()
	_, err := loader.Load(path)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}

func TestJSONLoaderLoadRejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	content := []byte(`{
		"http_address": "127.0.0.1:8080",
		"port": 8081,
		"wallet_path": "./wallet.json",
		"brain_chain_id": "./brain.js",
		"unexpected": true
	}`)

	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	loader := NewJSONLoader()
	_, err := loader.Load(path)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}
