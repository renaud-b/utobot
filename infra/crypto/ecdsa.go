package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"
)

type KeyPair struct {
	Version       int    `json:"v,omitempty"`
	Curve         string `json:"curve,omitempty"`
	PubCompressed bool   `json:"pub_compressed,omitempty"`
	PrivateKeyHex string `json:"priv_hex"`
	PublicKeyHex  string `json:"pub_hex"`
	Address       string `json:"address"`
	CreatedAt     string `json:"created_at,omitempty"`
}

var secp256k1 = newSecp256k1()

func timeNowISO() string {
	return fmt.Sprintf("%s", time.Now().Format(time.RFC3339))
}

func GenerateKeyPair() (KeyPair, error) {
	privateKey, err := ecdsa.GenerateKey(secp256k1, rand.Reader)
	if err != nil {
		return KeyPair{}, fmt.Errorf("generate secp256k1 keypair: %w", err)
	}

	publicKey := elliptic.Marshal(secp256k1, privateKey.X, privateKey.Y)
	publicKeyHex := hex.EncodeToString(publicKey)

	return KeyPair{
		Version:       1,
		Curve:         "secp256k1",
		PubCompressed: false,
		PrivateKeyHex: strings.ToUpper(privateKey.D.Text(16)),
		PublicKeyHex:  publicKeyHex,
		Address:       DeriveAddress(publicKeyHex),
		CreatedAt:     timeNowISO(),
	}, nil
}

func LoadFromFile(path string) (KeyPair, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return KeyPair{}, fmt.Errorf("read wallet file: %w", err)
	}
	return LoadFromB64String(string(bytes))
}

func LoadFromB64String(b64String string) (KeyPair, error) {
	bytes, err := base64.StdEncoding.DecodeString(b64String)
	if err != nil {
		return KeyPair{}, fmt.Errorf("decode base64 string: %w", err)
	}
	var kp KeyPair
	if err := json.Unmarshal(bytes, &kp); err != nil {
		return KeyPair{}, fmt.Errorf("decode wallet file: %w", err)
	}

	derivedAddress := DeriveAddress(kp.PublicKeyHex)
	if kp.Address != derivedAddress {
		return KeyPair{}, fmt.Errorf("address mismatch: expected %s, got %s", derivedAddress, kp.Address)
	}

	return kp, nil
}

func SaveToFile(kp KeyPair, path string) error {
	bytes, err := json.MarshalIndent(kp, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal wallet file: %w", err)
	}

	b64String := base64.StdEncoding.EncodeToString(bytes)

	if err := os.WriteFile(path, []byte(b64String), 0o600); err != nil {
		return fmt.Errorf("write wallet file: %w", err)
	}

	return nil
}

func SignMessage(privateKeyHex string, messageHashHex string) (string, error) {
	privateKey, err := privateKeyFromHex(privateKeyHex)
	if err != nil {
		return "", err
	}

	hashBytes, err := decodeHex(messageHashHex)
	if err != nil {
		return "", fmt.Errorf("decode message hash: %w", err)
	}

	signature, err := ecdsa.SignASN1(rand.Reader, privateKey, hashBytes)
	if err != nil {
		return "", fmt.Errorf("sign message hash: %w", err)
	}

	return hex.EncodeToString(signature), nil
}

func VerifySignature(publicKeyHex string, messageHashHex string, signatureHex string) (bool, error) {
	publicKey, err := publicKeyFromHex(publicKeyHex)
	if err != nil {
		return false, err
	}

	hashBytes, err := decodeHex(messageHashHex)
	if err != nil {
		return false, fmt.Errorf("decode message hash: %w", err)
	}

	signature, err := decodeHex(signatureHex)
	if err != nil {
		return false, fmt.Errorf("decode signature: %w", err)
	}

	if len(signature) == 64 {
		return verifyCompactSignature(publicKey, hashBytes, signature)
	}

	return ecdsa.VerifyASN1(publicKey, hashBytes, signature), nil
}

func verifyCompactSignature(publicKey *ecdsa.PublicKey, hashBytes []byte, signature []byte) (bool, error) {
	if len(signature) != 64 {
		return false, fmt.Errorf("compact signature must be 64 bytes")
	}

	r := new(big.Int).SetBytes(signature[:32])
	s := new(big.Int).SetBytes(signature[32:])
	if r.Sign() <= 0 || s.Sign() <= 0 {
		return false, nil
	}

	return ecdsa.Verify(publicKey, hashBytes, r, s), nil
}

func DeriveAddress(publicKeyHex string) string {
	publicKeyBytes, err := decodeHex(publicKeyHex)
	if err != nil {
		return "0x"
	}

	sum := sha256.Sum256(publicKeyBytes)
	hashHex := hex.EncodeToString(sum[:])
	if len(hashHex) > 40 {
		hashHex = hashHex[len(hashHex)-40:]
	}

	return "0x" + hashHex
}

func privateKeyFromHex(privateKeyHex string) (*ecdsa.PrivateKey, error) {
	d, ok := new(big.Int).SetString(trimHexPrefix(privateKeyHex), 16)
	if !ok {
		return nil, fmt.Errorf("decode private key")
	}
	if d.Sign() <= 0 || d.Cmp(secp256k1.Params().N) >= 0 {
		return nil, fmt.Errorf("private key out of secp256k1 range")
	}

	x, y := secp256k1.ScalarBaseMult(d.Bytes())
	if x == nil || y == nil {
		return nil, fmt.Errorf("reconstruct public key")
	}

	return &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{
			Curve: secp256k1,
			X:     x,
			Y:     y,
		},
		D: d,
	}, nil
}

func publicKeyFromHex(publicKeyHex string) (*ecdsa.PublicKey, error) {
	bytes, err := decodeHex(publicKeyHex)
	if err != nil {
		return nil, fmt.Errorf("decode public key: %w", err)
	}

	x, y := elliptic.Unmarshal(secp256k1, bytes)
	if x == nil || y == nil {
		return nil, fmt.Errorf("invalid secp256k1 public key")
	}

	return &ecdsa.PublicKey{
		Curve: secp256k1,
		X:     x,
		Y:     y,
	}, nil
}

func decodeHex(value string) ([]byte, error) {
	return hex.DecodeString(trimHexPrefix(value))
}

func trimHexPrefix(value string) string {
	return strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(value), "0x"), "0X")
}

type secp256k1Curve struct {
	params *elliptic.CurveParams
}

func newSecp256k1() elliptic.Curve {
	p := new(elliptic.CurveParams)
	p.P = mustHexInt("FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEFFFFFC2F")
	p.N = mustHexInt("FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEBAAEDCE6AF48A03BBFD25E8CD0364141")
	p.B = big.NewInt(7)
	p.Gx = mustHexInt("79BE667EF9DCBBAC55A06295CE870B07029BFCDB2DCE28D959F2815B16F81798")
	p.Gy = mustHexInt("483ADA7726A3C4655DA4FBFC0E1108A8FD17B448A68554199C47D08FFB10D4B8")
	p.BitSize = 256
	p.Name = "secp256k1"
	return &secp256k1Curve{params: p}
}

func mustHexInt(value string) *big.Int {
	n, ok := new(big.Int).SetString(value, 16)
	if !ok {
		panic("invalid secp256k1 parameter")
	}
	return n
}

func (c *secp256k1Curve) Params() *elliptic.CurveParams {
	return c.params
}

func (c *secp256k1Curve) IsOnCurve(x, y *big.Int) bool {
	if x == nil || y == nil || x.Sign() < 0 || y.Sign() < 0 || x.Cmp(c.params.P) >= 0 || y.Cmp(c.params.P) >= 0 {
		return false
	}

	left := new(big.Int).Mul(y, y)
	left.Mod(left, c.params.P)

	right := new(big.Int).Mul(x, x)
	right.Mul(right, x)
	right.Add(right, c.params.B)
	right.Mod(right, c.params.P)

	return left.Cmp(right) == 0
}

func (c *secp256k1Curve) Add(x1, y1, x2, y2 *big.Int) (*big.Int, *big.Int) {
	if x1 == nil || y1 == nil {
		return copyPoint(x2, y2)
	}
	if x2 == nil || y2 == nil {
		return copyPoint(x1, y1)
	}

	p := c.params.P
	if x1.Cmp(x2) == 0 {
		ySum := new(big.Int).Add(y1, y2)
		ySum.Mod(ySum, p)
		if ySum.Sign() == 0 {
			return nil, nil
		}
		return c.Double(x1, y1)
	}

	numerator := new(big.Int).Sub(y2, y1)
	numerator.Mod(numerator, p)
	denominator := new(big.Int).Sub(x2, x1)
	denominator.Mod(denominator, p)
	denominator.ModInverse(denominator, p)

	lambda := numerator.Mul(numerator, denominator)
	lambda.Mod(lambda, p)

	x3 := new(big.Int).Mul(lambda, lambda)
	x3.Sub(x3, x1)
	x3.Sub(x3, x2)
	x3.Mod(x3, p)

	y3 := new(big.Int).Sub(x1, x3)
	y3.Mul(lambda, y3)
	y3.Sub(y3, y1)
	y3.Mod(y3, p)

	return x3, y3
}

func (c *secp256k1Curve) Double(x1, y1 *big.Int) (*big.Int, *big.Int) {
	if x1 == nil || y1 == nil || y1.Sign() == 0 {
		return nil, nil
	}

	p := c.params.P
	numerator := new(big.Int).Mul(x1, x1)
	numerator.Mul(numerator, big.NewInt(3))
	numerator.Mod(numerator, p)

	denominator := new(big.Int).Lsh(y1, 1)
	denominator.Mod(denominator, p)
	denominator.ModInverse(denominator, p)

	lambda := numerator.Mul(numerator, denominator)
	lambda.Mod(lambda, p)

	x3 := new(big.Int).Mul(lambda, lambda)
	twoX1 := new(big.Int).Lsh(x1, 1)
	x3.Sub(x3, twoX1)
	x3.Mod(x3, p)

	y3 := new(big.Int).Sub(x1, x3)
	y3.Mul(lambda, y3)
	y3.Sub(y3, y1)
	y3.Mod(y3, p)

	return x3, y3
}

func (c *secp256k1Curve) ScalarMult(x1, y1 *big.Int, k []byte) (*big.Int, *big.Int) {
	var x, y *big.Int
	for _, b := range k {
		for bit := 7; bit >= 0; bit-- {
			x, y = c.Double(x, y)
			if b&(1<<uint(bit)) != 0 {
				x, y = c.Add(x, y, x1, y1)
			}
		}
	}
	return x, y
}

func (c *secp256k1Curve) ScalarBaseMult(k []byte) (*big.Int, *big.Int) {
	return c.ScalarMult(c.params.Gx, c.params.Gy, k)
}

func copyPoint(x, y *big.Int) (*big.Int, *big.Int) {
	if x == nil || y == nil {
		return nil, nil
	}
	return new(big.Int).Set(x), new(big.Int).Set(y)
}

/**
CPP Code:
#pragma once
#include <openssl/evp.h>
#include <openssl/core_names.h>
#include <openssl/param_build.h>
#include <vector>
#include <string>
#include <sstream>
#include <memory>
#include <openssl/sha.h>
#include <nlohmann/json.hpp>
#include <fstream>
#include "../../common/Helper.hpp"

namespace utx::infra::wallet {

    struct KeyPair {
        std::string private_key_hex;
        std::string public_key_hex;
        std::string address;
    };

    // Smart pointer pour gestion auto de la mémoire OpenSSL
    using ScopedEVP_PKEY = std::unique_ptr<EVP_PKEY, decltype(&EVP_PKEY_free)>;
    using ScopedEVP_PKEY_CTX = std::unique_ptr<EVP_PKEY_CTX, decltype(&EVP_PKEY_CTX_free)>;
    using ScopedOSSL_PARAM_BLD = std::unique_ptr<OSSL_PARAM_BLD, decltype(&OSSL_PARAM_BLD_free)>;
    using ScopedOSSL_PARAM = std::unique_ptr<OSSL_PARAM, decltype(&OSSL_PARAM_free)>;

    class WalletHelper {
    public:
        // ---------------------------------------------------------------------
        // GENERATION (OpenSSL 3.0 Style)
        // ---------------------------------------------------------------------
        static KeyPair generate_keypair() {
            // 1. Generate Key using the High-Level "Q" API (Quick)
            EVP_PKEY* pkey_raw = EVP_PKEY_Q_keygen(nullptr, nullptr, "EC", "secp256k1");
            if (!pkey_raw) throw std::runtime_error("Key generation failed");
            const ScopedEVP_PKEY pkey(pkey_raw, EVP_PKEY_free);

            // 2. Extract Private Key (BigNum to Hex)
            BIGNUM* priv_bn = nullptr;
            if (!EVP_PKEY_get_bn_param(pkey.get(), OSSL_PKEY_PARAM_PRIV_KEY, &priv_bn)) {
                throw std::runtime_error("Failed to extract private key");
            }
            char* priv_hex = BN_bn2hex(priv_bn);
            std::string priv_str(priv_hex);
            OPENSSL_free(priv_hex);
            BN_free(priv_bn);

            // 3. Extract Public Key (Point Octets to Hex)
            // OpenSSL 3 exports pubkey as an Octet String for EC
            size_t pub_len = 0;
            if (!EVP_PKEY_get_octet_string_param(pkey.get(), OSSL_PKEY_PARAM_PUB_KEY, nullptr, 0, &pub_len)) {
                throw std::runtime_error("Failed to get pubkey length");
            }
            std::vector<unsigned char> pub_bytes(pub_len);
            if (!EVP_PKEY_get_octet_string_param(pkey.get(), OSSL_PKEY_PARAM_PUB_KEY, pub_bytes.data(), pub_len, nullptr)) {
                throw std::runtime_error("Failed to extract public key");
            }

            KeyPair kp;
            kp.private_key_hex = priv_str;
            kp.public_key_hex = common::bytes_to_hex(pub_bytes);
            kp.address = derive_address(kp.public_key_hex);

            return kp;
        }

        static bool save_to_file(const KeyPair& kp, const std::string& path) {
            try {
                nlohmann::json j = {
                    {"address", kp.address},
                    {"public_key", kp.public_key_hex},
                    {"private_key", kp.private_key_hex}
                };
                std::ofstream file(path);
                file << j.dump(4);
                return true;
            } catch (...) { return false; }
        }

        static KeyPair load_from_file(const std::string& path) {
            std::ifstream file(path);
            nlohmann::json j;
            file >> j;
            if (j.contains("private_key") && j.contains("public_key") && j.contains("address")) {
                return {
                    j.at("private_key").get<std::string>(),
                    j.at("public_key").get<std::string>(),
                    j.at("address").get<std::string>()
                };
            } if (j.contains("private_key_hex") && j.contains("public_key_hex") && j.contains("address")) {
                return {
                    j.at("private_key_hex").get<std::string>(),
                    j.at("public_key_hex").get<std::string>(),
                    j.at("address").get<std::string>()
                };
            }
            throw std::runtime_error("Invalid wallet file format");
        }

        // ---------------------------------------------------------------------
        // SIGNATURE (OpenSSL 3.0 Style)
        // ---------------------------------------------------------------------
        static std::string sign_message(const std::string& private_key_hex, const std::string& message_hash_hex) {
            // 1. Rebuild Keypair from Hex Private Key
            const auto pkey = create_pkey_from_priv_hex(private_key_hex);

            // 2. Initialize Signing Context
            EVP_PKEY_CTX* ctx_raw = EVP_PKEY_CTX_new(pkey.get(), nullptr);
            if (!ctx_raw) throw std::runtime_error("Failed to create context");
            const ScopedEVP_PKEY_CTX ctx(ctx_raw, EVP_PKEY_CTX_free);

            if (EVP_PKEY_sign_init(ctx.get()) <= 0) throw std::runtime_error("Sign init failed");

            // 3. Sign the Hash
            const std::vector<unsigned char> hash_bytes = common::hex_to_bytes(message_hash_hex);

            size_t sig_len = 0;
            // First call to get length
            if (EVP_PKEY_sign(ctx.get(), nullptr, &sig_len, hash_bytes.data(), hash_bytes.size()) <= 0) {
                throw std::runtime_error("Sign sizing failed");
            }

            std::vector<unsigned char> signature(sig_len);
            // Second call to sign
            if (EVP_PKEY_sign(ctx.get(), signature.data(), &sig_len, hash_bytes.data(), hash_bytes.size()) <= 0) {
                throw std::runtime_error("Signing failed");
            }

            // Resize to actual length (DER format can vary in size)
            signature.resize(sig_len);
            return common::bytes_to_hex(signature);
        }
        static std::string derive_address(const std::string& pub_hex) {
            // 1. Convertir la string Hex en Bytes bruts
            // "04AF..." -> [0x04, 0xAF...]
            std::vector<unsigned char> pub_bytes = common::hex_to_bytes(pub_hex);

            // 2. Hacher les bytes bruts (Standard Ethereum/Bitcoin)
            unsigned char hash[SHA256_DIGEST_LENGTH];
            SHA256(pub_bytes.data(), pub_bytes.size(), hash);

            // 3. Convertir le hash résultant en Hex
            std::string hash_hex = common::bytes_to_hex(std::vector<unsigned char>(hash, hash + SHA256_DIGEST_LENGTH));

            // 4. Garder les 40 derniers caractères (20 derniers bytes)
            // SHA256 fait 64 chars hex. On prend les 40 derniers.
            if (hash_hex.length() > 40) {
                return "0x" + hash_hex.substr(hash_hex.length() - 40);
            }
            return "0x" + hash_hex;
        }
        // ---------------------------------------------------------------------
        // HELPERS
        // ---------------------------------------------------------------------
    private:
        static ScopedEVP_PKEY create_pkey_from_priv_hex(const std::string& priv_hex) {
            BIGNUM* priv_bn = BN_new();
            BN_hex2bn(&priv_bn, priv_hex.c_str());

            OSSL_PARAM_BLD* bld = OSSL_PARAM_BLD_new();
            OSSL_PARAM_BLD_push_utf8_string(bld, "group", "secp256k1", 0);
            OSSL_PARAM_BLD_push_BN(bld, "priv", priv_bn);

            OSSL_PARAM* params = OSSL_PARAM_BLD_to_param(bld);
            OSSL_PARAM_BLD_free(bld);
            BN_free(priv_bn);

            EVP_PKEY_CTX* ctx = EVP_PKEY_CTX_new_from_name(nullptr, "EC", nullptr);
            EVP_PKEY_fromdata_init(ctx);
            EVP_PKEY* pkey = nullptr;
            EVP_PKEY_fromdata(ctx, &pkey, EVP_PKEY_KEYPAIR, params);

            EVP_PKEY_CTX_free(ctx);
            OSSL_PARAM_free(params);

            if(!pkey) throw std::runtime_error("Failed to reconstruct private key");
            return {pkey, EVP_PKEY_free};
        }

    };

    inline void to_json(nlohmann::json& j, const KeyPair& kp) {
        // output as JWK
        j = nlohmann::json{
            {"private_key_hex", kp.private_key_hex},
            {"public_key_hex", kp.public_key_hex},
            {"address", kp.address}
        };
    }

    inline void from_json(const nlohmann::json& j, KeyPair& kp) {
        j.at("private_key_hex").get_to(kp.private_key_hex);
        j.at("public_key_hex").get_to(kp.public_key_hex);
        j.at("address").get_to(kp.address);
    }
}

*/
