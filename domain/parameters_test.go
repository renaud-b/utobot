package domain

import "testing"

func TestParametersValidate(t *testing.T) {
	t.Run("valid parameters", func(t *testing.T) {
		params := Parameters{
			HTTPAddress:  "127.0.0.1:8080",
			Port:         8081,
			WalletPath:   "./wallet.json",
			BrainChainID: "./brain.js",
		}

		if err := params.Validate(); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("missing brain path", func(t *testing.T) {
		params := Parameters{
			HTTPAddress: "127.0.0.1:8080",
			Port:        8081,
			WalletPath:  "./wallet.json",
		}

		if err := params.Validate(); err == nil {
			t.Fatal("expected an error, got nil")
		}
	})

	t.Run("missing port", func(t *testing.T) {
		params := Parameters{
			HTTPAddress:  "127.0.0.1:8080",
			WalletPath:   "./wallet.json",
			BrainChainID: "./brain.js",
		}

		if err := params.Validate(); err == nil {
			t.Fatal("expected an error, got nil")
		}
	})
}
