package security

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
)

func Equal(left, right []byte) bool {
	leftHash := sha256.Sum256(left)
	rightHash := sha256.Sum256(right)
	return subtle.ConstantTimeCompare(leftHash[:], rightHash[:]) == 1
}
func NewToken() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes[:]), nil
}
