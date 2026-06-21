package cryptoutil

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	privateKey := generatePrivateKey(t)
	payload := bytes.Repeat([]byte("metrics-payload-"), 100)

	encrypted, err := Encrypt(payload, &privateKey.PublicKey)
	if err != nil {
		t.Fatalf("Encrypt returned error: %v", err)
	}
	if bytes.Contains(encrypted, payload[:16]) {
		t.Fatalf("encrypted payload contains plaintext fragment")
	}

	decrypted, err := Decrypt(encrypted, privateKey)
	if err != nil {
		t.Fatalf("Decrypt returned error: %v", err)
	}
	if !bytes.Equal(decrypted, payload) {
		t.Fatalf("decrypted payload mismatch")
	}
}

func TestLoadPublicAndPrivateKeysFromPEM(t *testing.T) {
	privateKey := generatePrivateKey(t)
	dir := t.TempDir()

	publicPath := filepath.Join(dir, "public.pem")
	privatePath := filepath.Join(dir, "private.pem")
	writeFile(t, publicPath, encodePublicKeyPEM(t, &privateKey.PublicKey))
	writeFile(t, privatePath, encodePrivateKeyPEM(t, privateKey))

	publicKey, err := LoadPublicKey(publicPath)
	if err != nil {
		t.Fatalf("LoadPublicKey returned error: %v", err)
	}
	loadedPrivateKey, err := LoadPrivateKey(privatePath)
	if err != nil {
		t.Fatalf("LoadPrivateKey returned error: %v", err)
	}

	payload := []byte("json metrics")
	encrypted, err := Encrypt(payload, publicKey)
	if err != nil {
		t.Fatalf("Encrypt returned error: %v", err)
	}
	decrypted, err := Decrypt(encrypted, loadedPrivateKey)
	if err != nil {
		t.Fatalf("Decrypt returned error: %v", err)
	}
	if !bytes.Equal(decrypted, payload) {
		t.Fatalf("decrypted payload mismatch")
	}
}

func TestParseRejectsInvalidPEM(t *testing.T) {
	if _, err := ParsePublicKeyPEM([]byte("not pem")); err == nil {
		t.Fatal("expected public key parse error")
	}
	if _, err := ParsePrivateKeyPEM([]byte("not pem")); err == nil {
		t.Fatal("expected private key parse error")
	}
}

func TestDecryptRejectsInvalidPayload(t *testing.T) {
	privateKey := generatePrivateKey(t)

	if _, err := Decrypt([]byte("invalid"), privateKey); err == nil {
		t.Fatal("expected decrypt error")
	}
}

func generatePrivateKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}
	return privateKey
}

func encodePublicKeyPEM(t *testing.T, key *rsa.PublicKey) []byte {
	t.Helper()

	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		t.Fatalf("failed to marshal public key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}

func encodePrivateKeyPEM(t *testing.T, key *rsa.PrivateKey) []byte {
	t.Helper()

	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("failed to marshal private key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()

	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}
