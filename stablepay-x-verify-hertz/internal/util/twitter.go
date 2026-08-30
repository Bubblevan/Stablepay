package util

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var statusPathPattern = regexp.MustCompile(`^/([A-Za-z0-9_]+)/status/([0-9]+)$`)

func NormalizeTwitterHandle(handle string) string {
	handle = strings.TrimSpace(handle)
	handle = strings.TrimPrefix(handle, "@")
	return strings.ToLower(handle)
}

func ParseTweetURL(raw string) (handle string, statusID string, normalized string, err error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "", "", fmt.Errorf("invalid tweet_url")
	}

	host := strings.ToLower(u.Host)
	if host != "x.com" && host != "twitter.com" && host != "www.x.com" && host != "www.twitter.com" {
		return "", "", "", fmt.Errorf("tweet_url must be x.com or twitter.com")
	}

	matches := statusPathPattern.FindStringSubmatch(u.Path)
	if len(matches) != 3 {
		return "", "", "", fmt.Errorf("tweet_url path must be /{handle}/status/{id}")
	}

	handle = NormalizeTwitterHandle(matches[1])
	statusID = matches[2]
	normalized = fmt.Sprintf("https://x.com/%s/status/%s", handle, statusID)
	return handle, statusID, normalized, nil
}

func BuildExpectedVerificationText(did string) string {
	return fmt.Sprintf("I'm verifying my StablePay DID: %s", did)
}
