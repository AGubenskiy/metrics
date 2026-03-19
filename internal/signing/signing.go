package signing

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

const HeaderName = "HashSHA256"

func Hash(body []byte, key string) string {
	if key == "" {
		return ""
	}

	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func Valid(body []byte, key, expected string) bool {
	if key == "" || expected == "" {
		return false
	}

	actual := Hash(body, key)
	return hmac.Equal([]byte(actual), []byte(expected))
}
