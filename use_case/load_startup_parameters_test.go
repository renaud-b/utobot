package use_case

import (
	"errors"
	"testing"

	"utobot/domain"
)

type stubLoader struct {
	params domain.Parameters
	err    error
}

func (s stubLoader) Load(path string) (domain.Parameters, error) {
	return s.params, s.err
}

func TestLoadStartupParametersExecuteUsesLoaderResult(t *testing.T) {
	loader := stubLoader{
		params: domain.Parameters{
			HTTPAddress:  "127.0.0.1:8080",
			Port:         8081,
			WalletPath:   "./wallet.json",
			BrainChainID: "./brain.js",
			StateDBPath:  "./state.sqlite",
		},
	}

	uc := NewLoadStartupParameters(loader)
	params, err := uc.Execute("./config.json", "")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if params.BrainChainID != "./brain.js" {
		t.Fatalf("unexpected brain path: %q", params.BrainChainID)
	}
}

func TestLoadStartupParametersExecuteOverridesBrainPath(t *testing.T) {
	loader := stubLoader{
		params: domain.Parameters{
			HTTPAddress:  "127.0.0.1:8080",
			Port:         8081,
			WalletPath:   "./wallet.json",
			BrainChainID: "./brain.js",
			StateDBPath:  "./state.sqlite",
		},
	}

	uc := NewLoadStartupParameters(loader)
	params, err := uc.Execute("./config.json", "./override.js")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if params.BrainChainID != "./override.js" {
		t.Fatalf("unexpected brain path: %q", params.BrainChainID)
	}
}

func TestLoadStartupParametersExecuteReturnsLoaderError(t *testing.T) {
	loader := stubLoader{err: errors.New("boom")}

	uc := NewLoadStartupParameters(loader)
	_, err := uc.Execute("./config.json", "")
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}
