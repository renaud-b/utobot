package configfile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"utobot/domain"
	walletcrypto "utobot/infra/crypto"
)

type JSONLoader struct{}

func NewJSONLoader() JSONLoader {
	return JSONLoader{}
}

func (l JSONLoader) Load(path string) (domain.Parameters, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return domain.Parameters{}, fmt.Errorf("open config file: %w", err)
	}

	var params domain.Parameters
	if err := decodeStrict(bytes, &params); err != nil {
		return domain.Parameters{}, fmt.Errorf("decode config file: %w", err)
	}

	bytes, err = ensureEncryptionKey(bytes, path)
	if err != nil {
		return domain.Parameters{}, err
	}

	if err := decodeStrict(bytes, &params); err != nil {
		return domain.Parameters{}, fmt.Errorf("decode config file: %w", err)
	}

	if err := params.Validate(); err != nil {
		return domain.Parameters{}, err
	}

	return params, nil
}

func ensureEncryptionKey(data []byte, path string) ([]byte, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode config file: %w", err)
	}

	value, ok := raw["encryption_key"]
	if ok {
		var encoded string
		if err := json.Unmarshal(value, &encoded); err != nil {
			return nil, fmt.Errorf("decode encryption_key: %w", err)
		}
		if _, err := walletcrypto.DecodeEncryptionKey(encoded); err != nil {
			return nil, err
		}
		return data, nil
	}

	key, err := walletcrypto.GenerateEncryptionKey()
	if err != nil {
		return nil, err
	}

	encoded, err := walletcrypto.EncodeEncryptionKey(key)
	if err != nil {
		return nil, err
	}

	raw["encryption_key"] = mustMarshalRaw(encoded)
	updated, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal config file: %w", err)
	}
	updated = append(updated, '\n')

	if err := os.WriteFile(path, updated, 0o600); err != nil {
		return nil, fmt.Errorf("write config file: %w", err)
	}

	return updated, nil
}

func decodeStrict(data []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(out)
}

func mustMarshalRaw(value any) json.RawMessage {
	bytes, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return bytes
}
