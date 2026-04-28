package network

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

	walletcrypto "utobot/infra/crypto"
)

type DeployRequest struct {
	ChainID           string `json:"chain_id"`
	FilePath          string `json:"file_path,omitempty"`
	Kind              string `json:"kind,omitempty"`
	Projector         string `json:"projector,omitempty"`
	Content           string `json:"content"`
	CommitMessage     string `json:"commit_message"`
	ForceSnapshot     bool   `json:"force_snapshot,omitempty"`
	ForceGroupActions bool   `json:"force_group_actions,omitempty"`
	GroupActionSize   int    `json:"group_action_size,omitempty"`
}

type DeployResult struct {
	Success       bool   `json:"success"`
	PlanID        string `json:"plan_id"`
	BlocksEmitted int    `json:"blocks_emitted"`
}

type deployPrepareRequest struct {
	DeployRequest
	SenderAddress string `json:"sender_address"`
}

type DeployPlan struct {
	PlanID       string             `json:"plan_id"`
	Strategy     string             `json:"strategy"`
	ContentHash  string             `json:"content_hash"`
	Summary      string             `json:"summary"`
	Transactions []PreparedDeployTx `json:"transactions"`
}

type PreparedDeployTx struct {
	PayloadData string `json:"payload_data"`
}

type SignedTransaction struct {
	Sender          string `json:"sender"`
	Receiver        string `json:"receiver"`
	Amount          uint64 `json:"amount"`
	Nonce           uint64 `json:"nonce"`
	Data            string `json:"data"`
	SenderPublicKey string `json:"sender_pubkey"`
	Signature       string `json:"signature"`
}

type deploySubmitRequest struct {
	PlanID             string              `json:"plan_id"`
	ChainID            string              `json:"chain_id"`
	SignedTransactions []SignedTransaction `json:"signed_transactions"`
}

type deploySubmitResponse struct {
	Status        string `json:"status"`
	BlocksEmitted int    `json:"blocks_emitted"`
}

func (h *HttpClient) Deploy(req DeployRequest, wallet walletcrypto.KeyPair) (DeployResult, error) {
	plan, err := h.PrepareDeploy(req, wallet.Address)
	if err != nil {
		return DeployResult{}, err
	}

	if plan.PlanID == "" {
		return DeployResult{}, fmt.Errorf("invalid deploy plan: missing plan_id")
	}

	signedTransactions := make([]SignedTransaction, 0, len(plan.Transactions))
	for _, tx := range plan.Transactions {
		if tx.PayloadData == "" {
			return DeployResult{}, fmt.Errorf("invalid deploy plan: empty payload_data")
		}

		signedTx, err := BuildSignedTransaction(tx.PayloadData, wallet)
		if err != nil {
			return DeployResult{}, err
		}
		signedTransactions = append(signedTransactions, signedTx)
	}

	submit, err := h.SubmitDeploy(plan.PlanID, req.ChainID, signedTransactions)
	if err != nil {
		return DeployResult{}, err
	}

	return DeployResult{
		Success:       true,
		PlanID:        plan.PlanID,
		BlocksEmitted: submit.BlocksEmitted,
	}, nil
}

func (h *HttpClient) PrepareDeploy(req DeployRequest, senderAddress string) (DeployPlan, error) {
	body := deployPrepareRequest{
		DeployRequest: req,
		SenderAddress: senderAddress,
	}

	var plan DeployPlan
	if err := h.postJSON("/api/deploy/prepare", body, &plan); err != nil {
		return DeployPlan{}, fmt.Errorf("prepare deploy: %w", err)
	}

	if plan.Transactions == nil {
		return DeployPlan{}, fmt.Errorf("prepare deploy: invalid plan: missing transactions")
	}

	return plan, nil
}

func (h *HttpClient) SubmitDeploy(planID string, chainID string, signedTransactions []SignedTransaction) (deploySubmitResponse, error) {
	body := deploySubmitRequest{
		PlanID:             planID,
		ChainID:            chainID,
		SignedTransactions: signedTransactions,
	}

	var response deploySubmitResponse
	if err := h.postJSON("/api/deploy/submit", body, &response); err != nil {
		return deploySubmitResponse{}, fmt.Errorf("submit deploy: %w", err)
	}

	return response, nil
}

func BuildSignedTransaction(payload string, wallet walletcrypto.KeyPair) (SignedTransaction, error) {
	nonce, err := randomNonce()
	if err != nil {
		return SignedTransaction{}, err
	}

	tx := SignedTransaction{
		Sender:          wallet.Address,
		Receiver:        wallet.Address,
		Amount:          0,
		Nonce:           nonce,
		Data:            payload,
		SenderPublicKey: wallet.PublicKeyHex,
	}

	hash := sha256.Sum256([]byte(tx.serializeForSigning()))
	signature, err := walletcrypto.SignMessage(wallet.PrivateKeyHex, hex.EncodeToString(hash[:]))
	if err != nil {
		return SignedTransaction{}, fmt.Errorf("sign deploy transaction: %w", err)
	}

	tx.Signature = signature
	return tx, nil
}

func (tx SignedTransaction) serializeForSigning() string {
	return fmt.Sprintf("%s:%s:%d:%s:%d", tx.Sender, tx.Receiver, tx.Amount, tx.Data, tx.Nonce)
}

func (h *HttpClient) postJSON(path string, body any, out any) error {
	bytesBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request body: %w", err)
	}

	resp, err := h.client.Post(h.url+"/_p/8080"+path, "application/json", bytes.NewReader(bytesBody))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errorBody bytes.Buffer
		_, _ = errorBody.ReadFrom(resp.Body)
		return fmt.Errorf("status code %d: %s", resp.StatusCode, errorBody.String())
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response body: %w", err)
	}

	return nil
}

func randomNonce() (uint64, error) {
	var bytes [8]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return 0, fmt.Errorf("generate transaction nonce: %w", err)
	}

	return binary.BigEndian.Uint64(bytes[:]), nil
}

func (m *Mock) Deploy(req DeployRequest, wallet walletcrypto.KeyPair) (DeployResult, error) {
	args := m.Called(req, wallet)
	return args.Get(0).(DeployResult), args.Error(1)
}
