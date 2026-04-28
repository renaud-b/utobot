package crypto

import "testing"

func TestAES256GCMEncryptDecrypt(t *testing.T) {
	key, err := GenerateEncryptionKey()
	if err != nil {
		t.Fatalf("GenerateEncryptionKey() error = %v", err)
	}

	encrypted, err := EncryptAES256GCM(key, "secret")
	if err != nil {
		t.Fatalf("EncryptAES256GCM() error = %v", err)
	}
	if encrypted == "" || encrypted == "secret" {
		t.Fatalf("unexpected encrypted payload: %q", encrypted)
	}

	decrypted, err := DecryptAES256GCM(key, encrypted)
	if err != nil {
		t.Fatalf("DecryptAES256GCM() error = %v", err)
	}
	if decrypted != "secret" {
		t.Fatalf("unexpected plaintext: %q", decrypted)
	}
}

func TestDecodeEncryptionKeyRejectsWrongSize(t *testing.T) {
	_, err := DecodeEncryptionKey("AA==")
	if err == nil {
		t.Fatal("expected invalid key size error")
	}
}
