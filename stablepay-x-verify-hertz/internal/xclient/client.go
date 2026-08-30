package xclient

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"stablepay-x-verify-hertz/internal/config"
)

type Client struct {
	httpClient *http.Client
}

func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ========== Bearer Token API (Recommended for read-only) ==========

// TweetResult represents the result of fetching a tweet
type TweetResult struct {
	ID           string
	Text         string
	AuthorID     string
	AuthorHandle string
	Protected    bool
	CreatedAt    string
}

// GetTweetByID fetches a tweet by its ID using Bearer Token authentication
// This is the primary method for the MVP - it uses Bearer Token if available,
// otherwise falls back to OAuth 1.0a
func (c *Client) GetTweetByID(tweetID string) (*TweetResult, error) {
	// Try Bearer Token first (preferred for read-only)
	if config.C.XBearerToken != "" {
		return c.getTweetByIDWithBearer(tweetID)
	}

	// Fallback to OAuth 1.0a
	if config.C.XConsumerKey != "" && config.C.XAccessToken != "" {
		return c.getTweetByIDWithOAuth1(tweetID)
	}

	return nil, fmt.Errorf("no X API credentials configured")
}

// getTweetByIDWithBearer uses Bearer Token authentication
func (c *Client) getTweetByIDWithBearer(tweetID string) (*TweetResult, error) {
	apiURL := fmt.Sprintf("%s/tweets/%s?tweet.fields=author_id,created_at,text&expansions=author_id&user.fields=id,username,protected",
		config.C.XAPIBaseURL, tweetID)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+config.C.XBearerToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("tweet not found")
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("unauthorized - check your X_BEARER_TOKEN")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get tweet failed: %s - %s", resp.Status, string(body))
	}

	return c.parseTweetResponse(body)
}

// getTweetByIDWithOAuth1 uses OAuth 1.0a authentication
func (c *Client) getTweetByIDWithOAuth1(tweetID string) (*TweetResult, error) {
	apiURL := fmt.Sprintf("%s/tweets/%s", config.C.XAPIBaseURL, tweetID)

	// Build OAuth 1.0a parameters
	params := map[string]string{
		"tweet.fields": "author_id,created_at,text",
		"expansions":   "author_id",
		"user.fields":  "id,username,protected",
	}

	authHeader, err := c.buildOAuth1Header("GET", apiURL, params)
	if err != nil {
		return nil, fmt.Errorf("failed to build OAuth header: %w", err)
	}

	// Build full URL with query params
	queryURL := apiURL + "?" + c.encodeParams(params)

	req, err := http.NewRequest("GET", queryURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", authHeader)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("tweet not found")
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("unauthorized - check your OAuth 1.0a credentials")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get tweet failed: %s - %s", resp.Status, string(body))
	}

	return c.parseTweetResponse(body)
}

// parseTweetResponse parses the X API tweet response
func (c *Client) parseTweetResponse(body []byte) (*TweetResult, error) {
	var response struct {
		Data struct {
			ID        string `json:"id"`
			Text      string `json:"text"`
			AuthorID  string `json:"author_id"`
			CreatedAt string `json:"created_at"`
		} `json:"data"`
		Includes struct {
			Users []struct {
				ID        string `json:"id"`
				Username  string `json:"username"`
				Protected bool   `json:"protected"`
			} `json:"users"`
		} `json:"includes"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(response.Errors) > 0 {
		return nil, fmt.Errorf("API error: %s", response.Errors[0].Message)
	}

	result := &TweetResult{
		ID:        response.Data.ID,
		Text:      response.Data.Text,
		AuthorID:  response.Data.AuthorID,
		CreatedAt: response.Data.CreatedAt,
	}

	// Find author username from includes
	for _, user := range response.Includes.Users {
		if user.ID == response.Data.AuthorID {
			result.AuthorHandle = user.Username
			result.Protected = user.Protected
			break
		}
	}

	return result, nil
}

// ========== OAuth 1.0a Helper Methods ==========

// buildOAuth1Header creates an OAuth 1.0a Authorization header
func (c *Client) buildOAuth1Header(method, baseURL string, params map[string]string) (string, error) {
	// OAuth parameters
	oauthParams := map[string]string{
		"oauth_consumer_key":     config.C.XConsumerKey,
		"oauth_nonce":            c.generateNonce(),
		"oauth_signature_method": "HMAC-SHA1",
		"oauth_timestamp":        strconv.FormatInt(time.Now().Unix(), 10),
		"oauth_token":            config.C.XAccessToken,
		"oauth_version":          "1.0",
	}

	// Combine all parameters for signature
	sigParams := make(map[string]string)
	for k, v := range oauthParams {
		sigParams[k] = v
	}
	for k, v := range params {
		sigParams[k] = v
	}

	// Build signature base string
	signature, err := c.buildSignature(method, baseURL, sigParams)
	if err != nil {
		return "", err
	}

	oauthParams["oauth_signature"] = signature

	// Build Authorization header
	var headerParts []string
	for k, v := range oauthParams {
		headerParts = append(headerParts, fmt.Sprintf(`%s="%s"`, k, c.percentEncode(v)))
	}
	sort.Strings(headerParts)

	return "OAuth " + strings.Join(headerParts, ", "), nil
}

// buildSignature creates the OAuth 1.0a signature
func (c *Client) buildSignature(method, baseURL string, params map[string]string) (string, error) {
	// Build parameter string
	var keys []string
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var paramParts []string
	for _, k := range keys {
		paramParts = append(paramParts, c.percentEncode(k)+"="+c.percentEncode(params[k]))
	}
	paramString := strings.Join(paramParts, "&")

	// Build signature base string
	sigBase := method + "&" + c.percentEncode(baseURL) + "&" + c.percentEncode(paramString)

	// Build signing key
	signingKey := c.percentEncode(config.C.XConsumerSecret) + "&" + c.percentEncode(config.C.XAccessTokenSecret)

	// Create HMAC-SHA1 signature
	h := hmac.New(sha1.New, []byte(signingKey))
	h.Write([]byte(sigBase))
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	return signature, nil
}

// percentEncode implements OAuth 1.0a percent encoding (RFC 3986)
func (c *Client) percentEncode(s string) string {
	return strings.ReplaceAll(
		strings.ReplaceAll(
			url.QueryEscape(s),
			"+", "%20",
		),
		"%7E", "~",
	)
}

// encodeParams encodes parameters for URL query string
func (c *Client) encodeParams(params map[string]string) string {
	var parts []string
	for k, v := range params {
		parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(v))
	}
	sort.Strings(parts)
	return strings.Join(parts, "&")
}

// generateNonce generates a random nonce for OAuth 1.0a
func (c *Client) generateNonce() string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 32)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

// ========== Legacy OAuth 2.0 Methods (kept for backward compatibility) ==========

// BuildAuthorizeURL builds the X OAuth authorization URL with PKCE
func (c *Client) BuildAuthorizeURL(state, codeChallenge string) string {
	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", config.C.XClientID)
	params.Set("redirect_uri", config.C.XRedirectURI)
	params.Set("scope", config.C.XOAuthScopes)
	params.Set("state", state)
	params.Set("code_challenge", codeChallenge)
	params.Set("code_challenge_method", "S256")

	return fmt.Sprintf("%s?%s", config.C.XAuthorizeURL, params.Encode())
}

// TokenResponse represents the token endpoint response
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	TokenType    string `json:"token_type"`
}

// ExchangeCodeForToken exchanges authorization code for access token
func (c *Client) ExchangeCodeForToken(code, codeVerifier string) (*TokenResponse, error) {
	params := url.Values{}
	params.Set("grant_type", "authorization_code")
	params.Set("code", code)
	params.Set("redirect_uri", config.C.XRedirectURI)
	params.Set("code_verifier", codeVerifier)

	req, err := http.NewRequest("POST", config.C.XTokenURL, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// For confidential clients, use Basic Auth
	if config.C.XClientSecret != "" {
		auth := base64.StdEncoding.EncodeToString([]byte(config.C.XClientID + ":" + config.C.XClientSecret))
		req.Header.Set("Authorization", "Basic "+auth)
	} else {
		// For public clients, client_id is in the body
		params.Set("client_id", config.C.XClientID)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed: %s - %s", resp.Status, string(body))
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, err
	}

	return &tokenResp, nil
}

// RefreshAccessToken refreshes the access token using refresh token
func (c *Client) RefreshAccessToken(refreshToken string) (*TokenResponse, error) {
	params := url.Values{}
	params.Set("grant_type", "refresh_token")
	params.Set("refresh_token", refreshToken)

	req, err := http.NewRequest("POST", config.C.XTokenURL, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Basic Auth for confidential clients
	if config.C.XClientSecret != "" {
		auth := base64.StdEncoding.EncodeToString([]byte(config.C.XClientID + ":" + config.C.XClientSecret))
		req.Header.Set("Authorization", "Basic "+auth)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token refresh failed: %s - %s", resp.Status, string(body))
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, err
	}

	return &tokenResp, nil
}

// UserInfo represents the X user information
type UserInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Username  string `json:"username"`
	Protected bool   `json:"protected"`
	Verified  bool   `json:"verified,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
}

// UsersMeResponse represents the response from /2/users/me
type UsersMeResponse struct {
	Data UserInfo `json:"data"`
}

// GetMyUser fetches the authenticated user's information
func (c *Client) GetMyUser(accessToken string) (*UserInfo, error) {
	apiURL := fmt.Sprintf("%s/users/me", config.C.XAPIBaseURL)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get user failed: %s - %s", resp.Status, string(body))
	}

	var userResp UsersMeResponse
	if err := json.Unmarshal(body, &userResp); err != nil {
		return nil, err
	}

	return &userResp.Data, nil
}

// Legacy GetTweetByID with access token (for OAuth 2.0 flow)
func (c *Client) GetTweetByIDWithToken(tweetID, accessToken string) (*Tweet, *User, error) {
	apiURL := fmt.Sprintf("%s/tweets/%s?tweet.fields=author_id,created_at,text&expansions=author_id&user.fields=id,username,protected",
		config.C.XAPIBaseURL, tweetID)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, nil, err
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil, fmt.Errorf("tweet not found")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("get tweet failed: %s - %s", resp.Status, string(body))
	}

	var tweetResp TweetResponse
	if err := json.Unmarshal(body, &tweetResp); err != nil {
		return nil, nil, err
	}

	var author *User
	for i := range tweetResp.Includes.Users {
		if tweetResp.Includes.Users[i].ID == tweetResp.Data.AuthorID {
			author = &tweetResp.Includes.Users[i]
			break
		}
	}

	return &tweetResp.Data, author, nil
}

// Tweet represents a single tweet
type Tweet struct {
	ID        string `json:"id"`
	Text      string `json:"text"`
	AuthorID  string `json:"author_id"`
	CreatedAt string `json:"created_at,omitempty"`
}

// User represents user information in tweet expansion
type User struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	Protected bool   `json:"protected"`
}

// TweetResponse represents the response from /2/tweets/{id}
type TweetResponse struct {
	Data     Tweet `json:"data"`
	Includes struct {
		Users []User `json:"users"`
	} `json:"includes"`
}
