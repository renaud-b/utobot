package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"
	walletcrypto "utobot/infra/crypto"

	"utobot/infra/brain"
	"utobot/infra/httpserver"
	"utobot/infra/network"
	"utobot/infra/worker"

	"utobot/infra/configfile"
	"utobot/use_case"
)

func Run(args []string) error {
	configPath, brainOverride, err := parseArgs(args)
	if err != nil {
		return err
	}

	slog.Info("loading startup parameters", "config_path", configPath, "brain_override", brainOverride)

	loader := configfile.NewJSONLoader()
	uc := use_case.NewLoadStartupParameters(loader)

	params, err := uc.Execute(configPath, brainOverride)
	if err != nil {
		return err
	}

	slog.Info("startup parameters loaded",
		"http_address", params.HTTPAddress,
		"wallet_path", params.WalletPath,
		"brain_chain_id", params.BrainChainID,
	)

	client := network.NewHttpClient(params.HTTPAddress)
	scriptHeight, err := client.GetLastBlockIndex(params.BrainChainID)
	if err != nil {
		return err
	}

	js, err := client.FetchJavascript(params.BrainChainID)
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

	manager := brain.NewManagerWithEncryption(w, encryptionKey)
	if err := manager.LoadSource(params.BrainChainID, js); err != nil {
		return err
	}

	slog.Info("brain runtime initialized", "brain_path", manager.Path())

	scriptWorker := worker.New(client, manager, params.BrainChainID, params.ScriptHeightPath, 5*time.Second)
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
	brainOverride := ""
	if len(args) > 1 {
		// skip --debug if present
		for _, arg := range args[1:] {
			if arg == "--debug" {
				continue
			}
			brainOverride = arg
			break
		}
	}

	return configPath, brainOverride, nil
}
