package network

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	walletcrypto "utobot/infra/crypto"
)

func TestDeployPreparesSignsAndSubmits(t *testing.T) {
	wallet, err := walletcrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair() error = %v", err)
	}

	var prepareRequest deployPrepareRequest
	var submitRequest deploySubmitRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/_p/8080/api/deploy/prepare":
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected prepare method: %s", r.Method)
			}
			if err := json.NewDecoder(r.Body).Decode(&prepareRequest); err != nil {
				t.Fatalf("decode prepare request: %v", err)
			}
			_ = json.NewEncoder(w).Encode(DeployPlan{
				PlanID:      "plan-1",
				Strategy:    "incremental",
				ContentHash: "hash-1",
				Summary:     "summary",
				Transactions: []PreparedDeployTx{
					{PayloadData: "urn:pi:graph:action:first"},
					{PayloadData: "urn:pi:graph:action:second"},
				},
			})
		case "/_p/8080/api/deploy/submit":
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected submit method: %s", r.Method)
			}
			if err := json.NewDecoder(r.Body).Decode(&submitRequest); err != nil {
				t.Fatalf("decode submit request: %v", err)
			}
			_ = json.NewEncoder(w).Encode(deploySubmitResponse{
				Status:        "success",
				BlocksEmitted: 2,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewHttpClient(server.URL)
	result, err := client.Deploy(DeployRequest{
		ChainID:       "chain-1",
		FilePath:      "runtime.js",
		Kind:          "js",
		Content:       "module.exports = {}",
		CommitMessage: "deploy runtime",
	}, wallet)
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}

	if result != (DeployResult{Success: true, PlanID: "plan-1", BlocksEmitted: 2}) {
		t.Fatalf("unexpected result: %#v", result)
	}
	if prepareRequest.SenderAddress != wallet.Address {
		t.Fatalf("unexpected sender address: got %q want %q", prepareRequest.SenderAddress, wallet.Address)
	}
	if prepareRequest.ChainID != "chain-1" || prepareRequest.Content != "module.exports = {}" {
		t.Fatalf("unexpected prepare request: %#v", prepareRequest)
	}
	if submitRequest.PlanID != "plan-1" || submitRequest.ChainID != "chain-1" {
		t.Fatalf("unexpected submit request: %#v", submitRequest)
	}
	if len(submitRequest.SignedTransactions) != 2 {
		t.Fatalf("unexpected signed transaction count: %d", len(submitRequest.SignedTransactions))
	}

	for _, tx := range submitRequest.SignedTransactions {
		if tx.Sender != wallet.Address || tx.Receiver != wallet.Address || tx.SenderPublicKey != wallet.PublicKeyHex {
			t.Fatalf("unexpected signed transaction wallet fields: %#v", tx)
		}

		sum := sha256.Sum256([]byte(tx.serializeForSigning()))
		ok, err := walletcrypto.VerifySignature(wallet.PublicKeyHex, hex.EncodeToString(sum[:]), tx.Signature)
		if err != nil {
			t.Fatalf("VerifySignature() error = %v", err)
		}
		if !ok {
			t.Fatalf("signature did not verify for transaction: %#v", tx)
		}
	}
}

func TestDeployReturnsPrepareError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no plan", http.StatusBadRequest)
	}))
	defer server.Close()

	wallet, err := walletcrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair() error = %v", err)
	}

	client := NewHttpClient(server.URL)
	_, err = client.Deploy(DeployRequest{ChainID: "chain-1", Content: "content", CommitMessage: "commit"}, wallet)
	if err == nil {
		t.Fatal("expected deploy error")
	}
}
