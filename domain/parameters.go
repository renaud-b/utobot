package domain

import "fmt"

type Parameters struct {
	HTTPAddress      string `json:"http_address"`
	Port             int    `json:"port"`
	WalletPath       string `json:"wallet_path"`
	RuntimeChainID   string `json:"runtime_chain_id"`
	StateDBPath      string `json:"state_db_path"`
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
	if p.RuntimeChainID == "" {
		return fmt.Errorf("runtime_chain_id is required")
	}
	return nil
}
