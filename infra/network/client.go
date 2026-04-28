package network

import (
	"utobot/domain/graph"
	walletcrypto "utobot/infra/crypto"
)

type Client interface {
	FetchJavascript(chainID string) ([]byte, error)
	GetLastBlockIndex(ChainID string) (int, error)
	GetLastState(chainID string) (*graph.Graph, error)
	Deploy(req DeployRequest, wallet walletcrypto.KeyPair) (DeployResult, error)
}
