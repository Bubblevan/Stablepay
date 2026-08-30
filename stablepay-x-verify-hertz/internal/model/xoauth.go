package model

import "time"

// XOAuthSession stores the OAuth session state
type XOAuthSession struct {
	ID            string    `json:"id"`
	DID           string    `json:"did"`
	State         string    `json:"state"`
	CodeVerifier  string    `json:"code_verifier"`
	RedirectURI   string    `json:"redirect_uri"`
	Status        string    `json:"status"` // pending/authorized/expired
	XUserID       string    `json:"x_user_id"` // set after successful OAuth
	Username      string    `json:"username"`  // set after successful OAuth
	CreatedAt     time.Time `json:"created_at"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// XAccount stores the X user account information
type XAccount struct {
	XUserID        string    `json:"x_user_id"`
	Username       string    `json:"username"`
	Name           string    `json:"name"`
	Protected      bool      `json:"protected"`
	AccessToken    string    `json:"access_token"` // encrypted
	RefreshToken   string    `json:"refresh_token"` // encrypted, nullable
	TokenExpiresAt time.Time `json:"token_expires_at"`
	Scope          string    `json:"scope"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// XBinding stores the binding between DID and X account
type XBinding struct {
	DID           string    `json:"did"`
	XUserID       string    `json:"x_user_id"`
	Username      string    `json:"username"`
	TweetID       string    `json:"tweet_id"`
	TweetURL      string    `json:"tweet_url"`
	RewardTx      string    `json:"reward_tx"`
	VerifiedAt    time.Time `json:"verified_at"`
	CreatedAt     time.Time `json:"created_at"`
}

// XOAuthStatusResponse represents the OAuth status response
type XOAuthStatusResponse struct {
	Connected     bool   `json:"connected"`
	Username      string `json:"username,omitempty"`
	XUserID       string `json:"x_user_id,omitempty"`
	Protected     bool   `json:"protected,omitempty"`
	TokenExpired  bool   `json:"token_expired,omitempty"`
}

// XOAuthStartResponse represents the OAuth start response
type XOAuthStartResponse struct {
	AuthorizeURL string `json:"authorize_url"`
	State        string `json:"state"`
	ExpiresIn    int    `json:"expires_in"`
}
