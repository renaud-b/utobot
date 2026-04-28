package crypto

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateKeyPair(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair() error = %v", err)
	}

	if kp.PrivateKeyHex == "" {
		t.Fatal("expected private key")
	}
	if len(kp.PublicKeyHex) != 130 || !strings.HasPrefix(kp.PublicKeyHex, "04") {
		t.Fatalf("unexpected uncompressed public key: %q", kp.PublicKeyHex)
	}
	if got, want := kp.Address, DeriveAddress(kp.PublicKeyHex); got != want {
		t.Fatalf("unexpected address: got %q want %q", got, want)
	}
}

func TestSignMessage(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair() error = %v", err)
	}

	sum := sha256.Sum256([]byte("utobot"))
	signature, err := SignMessage(kp.PrivateKeyHex, hexString(sum[:]))
	if err != nil {
		t.Fatalf("SignMessage() error = %v", err)
	}

	ok, err := VerifySignature(kp.PublicKeyHex, hexString(sum[:]), signature)
	if err != nil {
		t.Fatalf("VerifySignature() error = %v", err)
	}
	if !ok {
		t.Fatal("expected signature to verify")
	}
}

func TestVerifyCompactSignature(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair() error = %v", err)
	}

	sum := sha256.Sum256([]byte("utobot"))
	privateKey, err := privateKeyFromHex(kp.PrivateKeyHex)
	if err != nil {
		t.Fatalf("privateKeyFromHex() error = %v", err)
	}

	r, s, err := ecdsa.Sign(rand.Reader, privateKey, sum[:])
	if err != nil {
		t.Fatalf("ecdsa.Sign() error = %v", err)
	}

	signature := compactSignatureHex(r, s)
	ok, err := VerifySignature(kp.PublicKeyHex, hexString(sum[:]), signature)
	if err != nil {
		t.Fatalf("VerifySignature() error = %v", err)
	}
	if !ok {
		t.Fatal("expected compact signature to verify")
	}
}

func TestSaveAndLoadWalletFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wallet.json")
	want, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair() error = %v", err)
	}

	if err := SaveToFile(want, path); err != nil {
		t.Fatalf("SaveToFile() error = %v", err)
	}

	got, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}
	if got != want {
		t.Fatalf("unexpected keypair: got %#v want %#v", got, want)
	}
}

func TestVerifySignature(t *testing.T) {
	signature := "3045022100c76ddc9cb73c8e327553759aea6112c3a389054389d274d4895f18ccf7723ae60220710a802657724110b461f1eea3a3b1ac6795c8832e77950b7baa106409e59563"
	publicKey := "04e6a7644db620c90c8783322160de8f436b201a0d18a6c422f418d7ef297ad9c2281b82186c36d6d8601df5cf721784e6d2ac6e65355d1da44a4555b7c489dbe1"
	//address := "0xe14b87e8e77a34b28bc2a316dbe2c309dfeba73b"
	payload := "aGVsbG8gd29ybGQh"
	ok, err := VerifySignature(publicKey, payload, signature)
	if err != nil {
		t.Fatalf("VerifySignature() error = %v", err)
	}
	if !ok {
		t.Fatal("expected signature to verify")
	}
}

func TestLoadFromB64String(t *testing.T) {
	b64String := "eyJ2IjoxLCJjdXJ2ZSI6InNlY3AyNTZrMSIsInByaXZfaGV4IjoiYWI2YTg1NWVlNTVhMGU0OGM3Y2U4ZjJhZTQ1N2U0MTZkNjAzMTBhMzU4OWFjNjMxNzIyYmE3OTUzYmY2Yzk4OCIsInB1Yl9oZXgiOiIwNDM4MTQ5M2U2MTg5Y2UxNThhMmZlNjdkZTA3ZjFhYzE3ZTAxY2YwZmRjYWRjNGUxZDJkMmViOWU3ODVmZjNhNmU2Nzk4NWI5MDcyOWQwM2NmZDljZWNjMGQ0YjNiNWJmYWE5ZThmNWI1NmQxMmVhYzRiZTNlMzcwOTE1N2JmMDQzIiwicHViX2NvbXByZXNzZWQiOmZhbHNlLCJhZGRyZXNzIjoiMHgwMGEzZTc1YTg0MDQyNjIxNGUzNTdiMjE5YzBiYjI4YWUxODRhOTAwIiwiY3JlYXRlZF9hdCI6IjIwMjYtMDMtMjJUMjI6MzA6MTUuNzczWiJ9"
	keyPair, err := LoadFromB64String(b64String)
	if err != nil {
		t.Fatalf("LoadFromB64String() error = %v", err)
	}
	assert.Equal(t, "ab6a855ee55a0e48c7ce8f2ae457e416d60310a3589ac631722ba7953bf6c988", keyPair.PrivateKeyHex)
	assert.Equal(t, "04381493e6189ce158a2fe67de07f1ac17e01cf0fdcadc4e1d2d2eb9e785ff3a6e67985b90729d03cfd9cecc0d4b3b5bfaa9e8f5b56d12eac4be3e3709157bf043", keyPair.PublicKeyHex)
	assert.Equal(t, "0x00a3e75a840426214e357b219c0bb28ae184a900", keyPair.Address)
}

func hexString(bytes []byte) string {
	const table = "0123456789abcdef"
	out := make([]byte, len(bytes)*2)
	for i, b := range bytes {
		out[i*2] = table[b>>4]
		out[i*2+1] = table[b&0x0f]
	}
	return string(out)
}

func compactSignatureHex(r, s *big.Int) string {
	rBytes := r.Bytes()
	sBytes := s.Bytes()

	padded := make([]byte, 64)
	copy(padded[32-len(rBytes):32], rBytes)
	copy(padded[64-len(sBytes):], sBytes)

	return hex.EncodeToString(padded)
}
