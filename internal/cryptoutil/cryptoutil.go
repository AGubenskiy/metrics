package cryptoutil

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	HeaderName  = "X-Encrypted"
	HeaderValue = "rsa-oaep-aes-gcm"

	magic = "MME1"

	version    byte = 1
	aesKeySize      = 32
)

// LoadPublicKey reads an RSA public key from PEM file.
func LoadPublicKey(path string) (*rsa.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read public key: %w", err)
	}

	return ParsePublicKeyPEM(data)
}

// LoadPrivateKey reads an RSA private key from PEM file.
func LoadPrivateKey(path string) (*rsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read private key: %w", err)
	}

	return ParsePrivateKeyPEM(data)
}

// ParsePublicKeyPEM parses RSA public keys in PKIX or PKCS#1 PEM format.
func ParsePublicKeyPEM(data []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(bytes.TrimSpace(data))
	if block == nil {
		return nil, errors.New("public key PEM block not found")
	}

	switch block.Type {
	case "PUBLIC KEY":
		key, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse public key: %w", err)
		}
		rsaKey, ok := key.(*rsa.PublicKey)
		if !ok {
			return nil, errors.New("public key is not RSA")
		}
		return rsaKey, nil
	case "RSA PUBLIC KEY":
		key, err := x509.ParsePKCS1PublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse RSA public key: %w", err)
		}
		return key, nil
	default:
		return nil, fmt.Errorf("unsupported public key type %q", block.Type)
	}
}

func ParsePrivateKeyPEM(data []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(bytes.TrimSpace(data))
	if block == nil {
		return nil, errors.New("private key PEM block not found")
	}

	switch block.Type {
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse private key: %w", err)
		}
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("private key is not RSA")
		}
		return rsaKey, nil
	case "RSA PRIVATE KEY":
		key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse RSA private key: %w", err)
		}
		return key, nil
	default:
		return nil, fmt.Errorf("unsupported private key type %q", block.Type)
	}
}

func Encrypt(payload []byte, publicKey *rsa.PublicKey) ([]byte, error) {
	if publicKey == nil {
		return nil, errors.New("public key is nil")
	}

	aesKey := make([]byte, aesKeySize)
	if _, err := io.ReadFull(rand.Reader, aesKey); err != nil {
		return nil, fmt.Errorf("generate AES key: %w", err)
	}

	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM cipher: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	encryptedKey, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, publicKey, aesKey, nil)
	if err != nil {
		return nil, fmt.Errorf("encrypt AES key: %w", err)
	}
	if len(encryptedKey) > int(^uint16(0)) {
		return nil, errors.New("encrypted AES key is too large")
	}
	if len(nonce) > int(^uint8(0)) {
		return nil, errors.New("too large")
	}

	ciphertext := gcm.Seal(nil, nonce, payload, nil)
	out := bytes.NewBuffer(make([]byte, 0, len(magic)+1+2+1+len(encryptedKey)+len(nonce)+len(ciphertext)))
	out.WriteString(magic)
	out.WriteByte(version)
	_ = binary.Write(out, binary.BigEndian, uint16(len(encryptedKey)))
	out.WriteByte(byte(len(nonce)))
	out.Write(encryptedKey)
	out.Write(nonce)
	out.Write(ciphertext)

	return out.Bytes(), nil
}

func Decrypt(payload []byte, privateKey *rsa.PrivateKey) ([]byte, error) {
	if privateKey == nil {
		return nil, errors.New("private key is nil")
	}
	if len(payload) < len(magic)+1+2+1 {
		return nil, errors.New("encrypted payload is too short")
	}
	if string(payload[:len(magic)]) != magic {
		return nil, errors.New("encrypted payload has invalid magic")
	}

	offset := len(magic)
	if payload[offset] != version {
		return nil, fmt.Errorf("unsupported encrypted payload version %d", payload[offset])
	}
	offset++

	keyLen := int(binary.BigEndian.Uint16(payload[offset : offset+2]))
	offset += 2
	nonceLen := int(payload[offset])
	offset++

	if keyLen == 0 {
		return nil, errors.New("encrypted AES key is empty")
	}
	if nonceLen == 0 {
		return nil, errors.New("nonce is empty")
	}
	if len(payload) < offset+keyLen+nonceLen {
		return nil, errors.New("encrypted payload is truncated")
	}

	encryptedKey := payload[offset : offset+keyLen]
	offset += keyLen
	nonce := payload[offset : offset+nonceLen]
	offset += nonceLen
	ciphertext := payload[offset:]
	if len(ciphertext) == 0 {
		return nil, errors.New("ciphertext is empty")
	}

	aesKey, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, privateKey, encryptedKey, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt AES key: %w", err)
	}
	if len(aesKey) != aesKeySize {
		return nil, fmt.Errorf("invalid AES key size %d", len(aesKey))
	}

	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM cipher: %w", err)
	}
	if nonceLen != gcm.NonceSize() {
		return nil, fmt.Errorf("invalid nonce size %d", nonceLen)
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt payload: %w", err)
	}

	return plaintext, nil
}
