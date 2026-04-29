package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"utobot/infra/network"
)

type RuntimeManager interface {
	LoadSource(path string, source []byte) error
}

type ScriptReloadWorker struct {
	client    network.Client
	manager   RuntimeManager
	chainID   string
	statePath string
	interval  time.Duration
	logger    *slog.Logger
}

func New(client network.Client, manager RuntimeManager, chainID, statePath string, interval time.Duration) *ScriptReloadWorker {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	if statePath == "" {
		statePath = ".utobot.script-height.json"
	}

	return &ScriptReloadWorker{
		client:    client,
		manager:   manager,
		chainID:   chainID,
		statePath: statePath,
		interval:  interval,
		logger:    slog.Default(),
	}
}

func (w *ScriptReloadWorker) Run(ctx context.Context) {
	w.syncAndLog()

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.syncAndLog()
		}
	}
}

func (w *ScriptReloadWorker) Bootstrap(height int) error {
	return writeHeight(w.statePath, height)
}

func (w *ScriptReloadWorker) SyncOnce() (bool, error) {
	currentHeight, err := w.client.GetLastBlockIndex(w.chainID)
	if err != nil {
		return false, fmt.Errorf("get last block index: %w", err)
	}

	lastHeight, exists, err := readHeight(w.statePath)
	if err != nil {
		return false, err
	}

	if !exists {
		return false, nil
	}

	if lastHeight == currentHeight {
		return false, nil
	}

	script, err := w.client.FetchJavascript(w.chainID)
	if err != nil {
		return false, fmt.Errorf("fetch javascript: %w", err)
	}

	if err := w.manager.LoadSource(w.chainID, script); err != nil {
		return false, err
	}

	if err := writeHeight(w.statePath, currentHeight); err != nil {
		return false, err
	}

	return true, nil
}

func (w *ScriptReloadWorker) syncAndLog() {
	reloaded, err := w.SyncOnce()
	if err != nil {
		w.logger.Error("script reload check failed", "chain_id", w.chainID, "error", err)
		return
	}

	if reloaded {
		w.logger.Info("script reloaded", "chain_id", w.chainID, "state_path", w.statePath)
	}
}

type heightState struct {
	Height int `json:"height"`
}

func readHeight(path string) (int, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("read script height: %w", err)
	}

	var state heightState
	if err := json.Unmarshal(data, &state); err != nil {
		return 0, false, fmt.Errorf("decode script height: %w", err)
	}

	return state.Height, true, nil
}

func writeHeight(path string, height int) error {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create script height directory: %w", err)
		}
	}

	data, err := json.Marshal(heightState{Height: height})
	if err != nil {
		return fmt.Errorf("encode script height: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write script height: %w", err)
	}

	return nil
}
