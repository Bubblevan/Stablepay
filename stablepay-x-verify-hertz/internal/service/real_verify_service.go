package service

import (
	"fmt"
	"strings"
	"time"

	"stablepay-x-verify-hertz/internal/config"
	"stablepay-x-verify-hertz/internal/model"
	"stablepay-x-verify-hertz/internal/store"
	"stablepay-x-verify-hertz/internal/util"
	"stablepay-x-verify-hertz/internal/xclient"

	"github.com/google/uuid"
)

// RealVerifyService handles real X API verification without OAuth callback
type RealVerifyService struct {
	xClient       *xclient.Client
	didStore      *store.DIDStore
	bindingStore  *store.BindingStore
	rewardStore   *store.RewardStore
	xBindingStore *store.XBindingStore
}

// NewRealVerifyService creates a new RealVerifyService
func NewRealVerifyService(
	didStore *store.DIDStore,
	bindingStore *store.BindingStore,
	rewardStore *store.RewardStore,
	xBindingStore *store.XBindingStore,
) *RealVerifyService {
	return &RealVerifyService{
		xClient:       xclient.NewClient(),
		didStore:      didStore,
		bindingStore:  bindingStore,
		rewardStore:   rewardStore,
		xBindingStore: xBindingStore,
	}
}

// VerifyRequest represents the verification request
type VerifyRequest struct {
	DID      string
	TweetURL string
}

// VerifyResponse represents the verification response
type VerifyResponse struct {
	Success       bool   `json:"success"`
	TwitterHandle string `json:"twitter_handle,omitempty"`
	RewardTx      string `json:"reward_tx,omitempty"`
	Message       string `json:"message"`
}

// Verify performs the real X API verification
func (s *RealVerifyService) Verify(req VerifyRequest) (*VerifyResponse, error) {
	// 1. Validate DID format
	if !util.IsValidDID(req.DID) {
		return nil, fmt.Errorf("invalid_did")
	}

	// 2. Check if DID exists
	_, ok := s.didStore.Get(req.DID)
	if !ok {
		return nil, fmt.Errorf("did_not_found")
	}

	// 3. Parse tweet URL to get tweet ID and handle
	handle, tweetID, normalizedURL, err := util.ParseTweetURL(req.TweetURL)
	if err != nil {
		return nil, fmt.Errorf("invalid_tweet_url: %w", err)
	}

	// 4. Check if DID is already bound to another X account
	if existingBinding, exists := s.xBindingStore.GetByDID(req.DID); exists {
		return nil, fmt.Errorf("did_already_bound: already bound to @%s", existingBinding.Username)
	}
	// Also check legacy binding store
	if existingBinding, exists := s.bindingStore.GetByDID(req.DID); exists {
		return nil, fmt.Errorf("did_already_bound: already bound to @%s", existingBinding.TwitterHandle)
	}

	// 5. Check if X account is already bound to another DID
	if existingBinding, exists := s.xBindingStore.GetByUsername(handle); exists {
		return nil, fmt.Errorf("twitter_already_bound: @%s already bound to %s", handle, existingBinding.DID)
	}
	// Also check legacy binding store
	if existingDID, exists := s.bindingStore.GetDIDByHandle(handle); exists {
		return nil, fmt.Errorf("twitter_already_bound: @%s already bound to %s", handle, existingDID)
	}

	// 6. Fetch tweet from X API
	tweet, err := s.xClient.GetTweetByID(tweetID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, fmt.Errorf("tweet_not_found")
		}
		return nil, fmt.Errorf("x_api_error: %w", err)
	}

	// 7. Check if tweet author matches the URL handle
	if util.NormalizeTwitterHandle(tweet.AuthorHandle) != handle {
		return nil, fmt.Errorf("tweet_author_mismatch")
	}

	// 8. Check if account is protected (private)
	if tweet.Protected {
		return nil, fmt.Errorf("twitter_account_protected")
	}

	// 9. Check if tweet contains required verification content
	expectedPrefix := config.C.XVerifyTweetPrefix
	if !strings.Contains(tweet.Text, req.DID) {
		return nil, fmt.Errorf("did_not_in_tweet: tweet text does not contain the DID")
	}
	if !strings.Contains(tweet.Text, expectedPrefix) {
		return nil, fmt.Errorf("invalid_tweet_content: tweet must contain '%s'", expectedPrefix)
	}

	// 10. Generate reward transaction
	rewardTx := "mock_reward_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	verifiedAt := time.Now().UTC()

	// 11. Save binding
	s.xBindingStore.Save(&model.XBinding{
		DID:        req.DID,
		XUserID:    tweet.AuthorID,
		Username:   tweet.AuthorHandle,
		TweetID:    tweetID,
		TweetURL:   normalizedURL,
		RewardTx:   rewardTx,
		VerifiedAt: verifiedAt,
		CreatedAt:  verifiedAt,
	})

	// 12. Save reward record
	didIdentity, _ := s.didStore.Get(req.DID)
	s.rewardStore.Save(model.RewardRecord{
		DID:        req.DID,
		Amount:     1,
		Currency:   "USDC",
		RewardTx:   rewardTx,
		IssuedAt:   verifiedAt,
		WalletAddr: didIdentity.WalletAddress,
	})

	return &VerifyResponse{
		Success:       true,
		TwitterHandle: "@" + tweet.AuthorHandle,
		RewardTx:      rewardTx,
		Message:       "Verification successful, 1 USDC sent",
	}, nil
}

// GetVerifyStatus checks if a DID has been verified
func (s *RealVerifyService) GetVerifyStatus(did string) (*model.VerifyStatusResponse, error) {
	if !util.IsValidDID(did) {
		return nil, fmt.Errorf("invalid_did")
	}

	// Check new X binding store first
	if binding, ok := s.xBindingStore.GetByDID(did); ok {
		username := "@" + binding.Username
		return &model.VerifyStatusResponse{
			Verified:      true,
			TwitterHandle: &username,
			RewardTx:      &binding.RewardTx,
		}, nil
	}

	// Check legacy binding store
	if binding, ok := s.bindingStore.GetByDID(did); ok {
		return &model.VerifyStatusResponse{
			Verified:      true,
			TwitterHandle: &binding.TwitterHandle,
			RewardTx:      &binding.RewardTx,
		}, nil
	}

	return &model.VerifyStatusResponse{
		Verified: false,
	}, nil
}
