package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"
	walletcrypto "utobot/infra/crypto"
	"utobot/infra/llm"

	"utobot/infra/httpserver"
	"utobot/infra/network"
	"utobot/infra/runtime"
	"utobot/infra/state"
	"utobot/infra/worker"

	"utobot/infra/configfile"
	"utobot/use_case"
)

func Run(args []string) error {
	configPath, runtimeOverride, err := parseArgs(args)
	if err != nil {
		return err
	}

	slog.Info("loading startup parameters", "config_path", configPath, "runtime_override", runtimeOverride)

	loader := configfile.NewJSONLoader()
	uc := use_case.NewLoadStartupParameters(loader)

	params, err := uc.Execute(configPath, runtimeOverride)
	if err != nil {
		return err
	}

	slog.Info("startup parameters loaded",
		"http_address", params.HTTPAddress,
		"wallet_path", params.WalletPath,
		"runtime_chain_id", params.RuntimeChainID,
		"state_db_path", params.StateDBPath,
	)

	client := network.NewHttpClient(params.HTTPAddress)
	scriptHeight, err := client.GetLastBlockIndex(params.RuntimeChainID)
	if err != nil {
		return err
	}

	js, err := client.FetchJavascript(params.RuntimeChainID)
	if err != nil {
		return err
	}

	slog.Info("fetched javascript", "length", len(js))

	w, err := walletcrypto.LoadFromFile(params.WalletPath)
	if err != nil {
		return err
	}

	encryptionKey, err := walletcrypto.DecodeEncryptionKey(params.EncryptionKey)
	if err != nil {
		return err
	}

	var llmConnector runtime.LLMConnector
	if params.LLMKey != "" {
		llmConnector, err = llm.NewOpenAIConnector(params.LLMKey)
		if err != nil {
			return err
		}
		slog.Info("llm connector configured", "provider", "openai")
	}

	var stateStore state.ContextStore = state.NewMemoryStore()
	if params.StateDBPath != "" {
		stateStore, err = state.NewSQLiteStore(params.StateDBPath)
		if err != nil {
			return err
		}
		defer stateStore.Close()
		slog.Info("state store configured", "backend", "sqlite", "path", params.StateDBPath)
	} else {
		slog.Info("state store configured", "backend", "memory")
	}

	manager := runtime.NewManagerWithStateStore(w, encryptionKey, llmConnector, stateStore)
	if err := manager.LoadSource(params.RuntimeChainID, js); err != nil {
		return err
	}

	slog.Info("js runtime initialized", "runtime_path", manager.Path())

	scriptWorker := worker.New(client, manager, params.RuntimeChainID, params.ScriptHeightPath, 5*time.Second)
	if err := scriptWorker.Bootstrap(scriptHeight); err != nil {
		return err
	}
	go scriptWorker.Run(context.Background())

	server := httpserver.New(params.Port, manager)
	slog.Info("starting http server", "addr", server.Addr())
	return server.Start()
}

func parseArgs(args []string) (string, string, error) {
	if len(args) == 0 {
		return "", "", fmt.Errorf("usage: utobot <config.json> [chain_id]")
	}

	configPath := args[0]
	runtimeOverride := ""
	if len(args) > 1 {
		// skip --debug if present
		for _, arg := range args[1:] {
			if arg == "--debug" {
				continue
			}
			runtimeOverride = arg
			break
		}
	}

	return configPath, runtimeOverride, nil
}
