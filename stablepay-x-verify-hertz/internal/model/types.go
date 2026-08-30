package model

import "time"

type VerifyTwitterRequest struct {
	DID      string `json:"did,required"`
	TweetURL string `json:"tweet_url,required"`
}

type VerifyTwitterResponse struct {
	Success       bool   `json:"success"`
	TwitterHandle string `json:"twitter_handle,omitempty"`
	RewardTx      string `json:"reward_tx,omitempty"`
	Message       string `json:"message"`
}

type ErrorResponse struct {
	Success bool   `json:"success"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type DIDIdentity struct {
	DID           string    `json:"did"`
	WalletAddress string    `json:"wallet_address"`
	CreatedAt     time.Time `json:"created_at"`
}

type MockCreateDIDRequest struct {
	DID           string `json:"did,required"`
	WalletAddress string `json:"wallet_address,required"`
}

type MockTweet struct {
	TweetURL     string    `json:"tweet_url"`
	AuthorHandle string    `json:"author_handle"`
	Text         string    `json:"text"`
	IsPublic     bool      `json:"is_public"`
	CreatedAt    time.Time `json:"created_at"`
}

type MockCreateTweetRequest struct {
	TweetURL     string `json:"tweet_url,required"`
	AuthorHandle string `json:"author_handle,required"`
	Text         string `json:"text,required"`
	IsPublic     bool   `json:"is_public"`
}

type Binding struct {
	DID           string    `json:"did"`
	TwitterHandle string    `json:"twitter_handle"`
	TweetURL      string    `json:"tweet_url"`
	RewardTx      string    `json:"reward_tx"`
	VerifiedAt    time.Time `json:"verified_at"`
}

type VerifyStatusResponse struct {
	Verified      bool    `json:"verified"`
	TwitterHandle *string `json:"twitter_handle,omitempty"`
	RewardTx      *string `json:"reward_tx,omitempty"`
}

type RewardRecord struct {
	DID        string    `json:"did"`
	Amount     float64   `json:"amount"`
	Currency   string    `json:"currency"`
	RewardTx   string    `json:"reward_tx"`
	IssuedAt   time.Time `json:"issued_at"`
	WalletAddr string    `json:"wallet_addr"`
}

type BalanceResponse struct {
	Balance  float64 `json:"balance"`
	Currency string  `json:"currency"`
}
