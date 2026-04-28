package domain

import "fmt"

type Parameters struct {
	HTTPAddress      string `json:"http_address"`
	Port             int    `json:"port"`
	WalletPath       string `json:"wallet_path"`
	BrainChainID     string `json:"brain_chain_id"`
	ScriptHeightPath string `json:"script_height_path"`
	EncryptionKey    string `json:"encryption_key"`
	LLMKey           string `json:"llm_key"`
}

func (p Parameters) Validate() error {
	if p.HTTPAddress == "" {
		return fmt.Errorf("http_address is required")
	}
	if p.Port <= 0 || p.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	if p.WalletPath == "" {
		return fmt.Errorf("wallet_path is required")
	}
	if p.BrainChainID == "" {
		return fmt.Errorf("brain_path is required")
	}
	return nil
}
