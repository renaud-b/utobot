package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	walletcrypto "utobot/infra/crypto"
)

func TestLoadConfiguredWallet(t *testing.T) {
	t.Run("omitted wallet is allowed", func(t *testing.T) {
		wallet, err := loadConfiguredWallet("")
		if err != nil {
			t.Fatalf("walletless startup: %v", err)
		}
		if wallet != (walletcrypto.KeyPair{}) {
			t.Fatalf("walletless startup unexpectedly created a key: %+v", wallet)
		}
	})

	t.Run("configured wallet is loaded", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "wallet.utx")
		want, err := walletcrypto.GenerateKeyPair()
		if err != nil {
			t.Fatalf("generate wallet: %v", err)
		}
		if err := walletcrypto.SaveToFile(want, path); err != nil {
			t.Fatalf("save wallet: %v", err)
		}

		got, err := loadConfiguredWallet(path)
		if err != nil {
			t.Fatalf("load configured wallet: %v", err)
		}
		if got != want {
			t.Fatalf("loaded wallet differs from configured wallet")
		}
	})

	t.Run("missing configured wallet fails", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing.utx")
		if _, err := loadConfiguredWallet(path); err == nil {
			t.Fatal("missing configured wallet should fail")
		}
	})

	t.Run("invalid configured wallet fails", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "invalid.utx")
		if err := os.WriteFile(path, []byte("not a wallet"), 0o600); err != nil {
			t.Fatalf("write invalid wallet: %v", err)
		}
		if _, err := loadConfiguredWallet(path); err == nil ||
			!strings.Contains(err.Error(), "decode base64") {
			t.Fatalf("invalid configured wallet should fail, got: %v", err)
		}
	})
}
