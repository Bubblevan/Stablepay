package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

func BodySHA256(rawBody []byte) string {
	sum := sha256.Sum256(rawBody)
	return hex.EncodeToString(sum[:])
}

func BuildCanonicalV01(method, path, rawQuery, bodySHA256 string) string {
	return fmt.Sprintf("%s\n%s\n%s\n%s", method, path, rawQuery, bodySHA256)
}
