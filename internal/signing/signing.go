package signing

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// HeaderName is the HTTP header used to transport a SHA-256 HMAC of the body.
const HeaderName = "HashSHA256"

// Hash returns a SHA-256 HMAC for body encoded as a hex string.
//
// If key is empty, Hash returns an empty string.
func Hash(body []byte, key string) string {
	if key == "" {
		return ""
	}

	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// Valid reports whether expected matches the HMAC of body calculated with key.
func Valid(body []byte, key, expected string) bool {
	if key == "" || expected == "" {
		return false
	}

	actual := Hash(body, key)
	return hmac.Equal([]byte(actual), []byte(expected))
}
