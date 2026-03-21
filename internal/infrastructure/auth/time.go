package auth

import (
	"fmt"
	"strconv"
	"time"
)

func ParseTimestampFlexible(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, fmt.Errorf("timestamp is empty")
	}
	if unixSec, err := strconv.ParseInt(value, 10, 64); err == nil {
		return time.Unix(unixSec, 0).UTC(), nil
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timestamp format")
	}
	return t.UTC(), nil
}
