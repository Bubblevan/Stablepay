package util

import "regexp"

var didPattern = regexp.MustCompile(`^did:solana:[1-9A-HJ-NP-Za-km-z]{16,64}$`)

func IsValidDID(did string) bool {
	return didPattern.MatchString(did)
}
