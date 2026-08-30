package service

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"stablepay-x-verify-hertz/internal/config"
	"stablepay-x-verify-hertz/internal/model"
	"stablepay-x-verify-hertz/internal/store"
	"stablepay-x-verify-hertz/internal/util"
	"stablepay-x-verify-hertz/internal/xclient"
)

type XOAuthService struct {
	sessionStore  *store.XOAuthSessionStore
	accountStore  *store.XAccountStore
	bindingStore  *store.XBindingStore
	didStore      *store.DIDStore
	xClient       *xclient.Client
}

func NewXOAuthService(
	sessionStore *store.XOAuthSessionStore,
	accountStore *store.XAccountStore,
	bindingStore *store.XBindingStore,
	didStore *store.DIDStore,
) *XOAuthService {
	return &XOAuthService{
		sessionStore: sessionStore,
		accountStore: accountStore,
		bindingStore: bindingStore,
		didStore:     didStore,
		xClient:      xclient.NewClient(),
	}
}

// StartOAuth initiates the OAuth flow
func (s *XOAuthService) StartOAuth(did string) (*model.XOAuthStartResponse, error) {
	// Validate DID
	if !util.IsValidDID(did) {
		return nil, fmt.Errorf("invalid_did")
	}

	// Check if DID exists
	if _, ok := s.didStore.Get(did); !ok {
		return nil, fmt.Errorf("did_not_found")
	}

	// Generate PKCE params
	state := util.GenerateState()
	codeVerifier := util.GenerateCodeVerifier()
	codeChallenge := util.GenerateCodeChallengeS256(codeVerifier)

	// Create session
	session := &model.XOAuthSession{
		ID:           uuid.NewString(),
		DID:          did,
		State:        state,
		CodeVerifier: codeVerifier,
		RedirectURI:  config.C.XRedirectURI,
		Status:       "pending",
		CreatedAt:    time.Now().UTC(),
		ExpiresAt:    time.Now().UTC().Add(5 * time.Minute),
	}

	s.sessionStore.Save(session)

	// Build authorize URL
	authorizeURL := s.xClient.BuildAuthorizeURL(state, codeChallenge)

	return &model.XOAuthStartResponse{
		AuthorizeURL: authorizeURL,
		State:        state,
		ExpiresIn:    300,
	}, nil
}

// HandleCallback processes the OAuth callback
func (s *XOAuthService) HandleCallback(code, state string) (string, error) {
	// Get session by state
	session, ok := s.sessionStore.GetByState(state)
	if !ok {
		return "", fmt.Errorf("invalid_state")
	}

	// Check if expired
	if time.Now().After(session.ExpiresAt) {
		s.sessionStore.Delete(state)
		return "", fmt.Errorf("session_expired")
	}

	// Exchange code for token
	tokenResp, err := s.xClient.ExchangeCodeForToken(code, session.CodeVerifier)
	if err != nil {
		return "", fmt.Errorf("token_exchange_failed: %w", err)
	}

	// Get user info
	userInfo, err := s.xClient.GetMyUser(tokenResp.AccessToken)
	if err != nil {
		return "", fmt.Errorf("get_user_failed: %w", err)
	}

	// Encrypt tokens
	encryptedAccessToken, err := util.Encrypt(tokenResp.AccessToken)
	if err != nil {
		return "", fmt.Errorf("encrypt_failed: %w", err)
	}

	var encryptedRefreshToken string
	if tokenResp.RefreshToken != "" {
		encryptedRefreshToken, err = util.Encrypt(tokenResp.RefreshToken)
		if err != nil {
			return "", fmt.Errorf("encrypt_failed: %w", err)
		}
	}

	// Save/update account
	account := &model.XAccount{
		XUserID:        userInfo.ID,
		Username:       userInfo.Username,
		Name:           userInfo.Name,
		Protected:      userInfo.Protected,
		AccessToken:    encryptedAccessToken,
		RefreshToken:   encryptedRefreshToken,
		TokenExpiresAt: time.Now().UTC().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
		Scope:          tokenResp.Scope,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}
	s.accountStore.Save(account)

	// Update session with X user info - this links DID to X account
	session.Status = "authorized"
	session.XUserID = userInfo.ID
	session.Username = userInfo.Username
	s.sessionStore.Update(session)

	// Build redirect URL
	redirectURL := fmt.Sprintf("%s?did=%s&oauth=success&username=%s",
		config.C.FrontendVerifyURL,
		url.QueryEscape(session.DID),
		url.QueryEscape(userInfo.Username),
	)

	return redirectURL, nil
}

// GetOAuthStatus returns the OAuth status for a DID
func (s *XOAuthService) GetOAuthStatus(did string) (*model.XOAuthStatusResponse, error) {
	// First check if there's a completed binding
	binding, ok := s.bindingStore.GetByDID(did)
	if ok {
		account, ok := s.accountStore.GetByXUserID(binding.XUserID)
		if ok {
			return &model.XOAuthStatusResponse{
				Connected:    true,
				Username:     account.Username,
				XUserID:      account.XUserID,
				Protected:    account.Protected,
				TokenExpired: time.Now().After(account.TokenExpiresAt),
			}, nil
		}
	}

	// Check if there's an active OAuth session
	session, ok := s.sessionStore.GetByDID(did)
	if !ok || session.Status != "authorized" {
		return &model.XOAuthStatusResponse{Connected: false}, nil
	}

	// Get account info from session
	account, ok := s.accountStore.GetByXUserID(session.XUserID)
	if !ok {
		return &model.XOAuthStatusResponse{Connected: false}, nil
	}

	return &model.XOAuthStatusResponse{
		Connected:    true,
		Username:     account.Username,
		XUserID:      account.XUserID,
		Protected:    account.Protected,
		TokenExpired: time.Now().After(account.TokenExpiresAt),
	}, nil
}

// VerifyTweet verifies a tweet for DID verification using real X API
func (s *XOAuthService) VerifyTweet(did, tweetURL string) (*model.VerifyTwitterResponse, error) {
	// Validate DID
	if !util.IsValidDID(did) {
		return nil, fmt.Errorf("invalid_did")
	}

	// Check if DID exists
	if _, ok := s.didStore.Get(did); !ok {
		return nil, fmt.Errorf("did_not_found")
	}

	// Check if DID is already bound
	if _, ok := s.bindingStore.GetByDID(did); ok {
		return nil, fmt.Errorf("did_already_bound")
	}

	// Get OAuth session for this DID
	session, ok := s.sessionStore.GetByDID(did)
	if !ok || session.Status != "authorized" {
		return nil, fmt.Errorf("x_oauth_not_connected")
	}

	// Get account using x_user_id from session
	account, ok := s.accountStore.GetByXUserID(session.XUserID)
	if !ok {
		return nil, fmt.Errorf("x_account_not_found")
	}

	// Decrypt access token
	accessToken, err := util.Decrypt(account.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("decrypt_failed")
	}

	// Parse tweet URL to get tweet ID
	tweetID, err := extractTweetID(tweetURL)
	if err != nil {
		return nil, fmt.Errorf("invalid_tweet_url")
	}

	// Fetch tweet from X API using OAuth 2.0 token
	tweet, author, err := s.xClient.GetTweetByIDWithToken(tweetID, accessToken)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, fmt.Errorf("tweet_not_found")
		}
		return nil, fmt.Errorf("x_api_error: %w", err)
	}

	// Verify tweet author matches OAuth user
	if tweet.AuthorID != account.XUserID {
		return nil, fmt.Errorf("tweet_author_mismatch")
	}

	// Verify tweet text contains DID
	if !strings.Contains(tweet.Text, did) {
		return nil, fmt.Errorf("did_not_in_tweet")
	}

	// Verify tweet text contains expected prefix
	expectedPrefix := strings.Replace(config.C.XVerifyTweetTemplate, "{DID}", "", 1)
	if !strings.Contains(tweet.Text, expectedPrefix) {
		return nil, fmt.Errorf("did_not_in_tweet")
	}

	// Check if account is protected
	if author != nil && author.Protected {
		return nil, fmt.Errorf("twitter_account_protected")
	}

	// Check if X account is already bound to another DID
	if existingBinding, ok := s.bindingStore.GetByXUserID(account.XUserID); ok && existingBinding.DID != did {
		return nil, fmt.Errorf("twitter_already_bound")
	}

	// Generate reward transaction
	rewardTx := "reward_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	verifiedAt := time.Now().UTC()

	// Save binding
	newBinding := &model.XBinding{
		DID:        did,
		XUserID:    account.XUserID,
		Username:   account.Username,
		TweetID:    tweetID,
		TweetURL:   tweetURL,
		RewardTx:   rewardTx,
		VerifiedAt: verifiedAt,
		CreatedAt:  verifiedAt,
	}
	s.bindingStore.Save(newBinding)

	return &model.VerifyTwitterResponse{
		Success:       true,
		TwitterHandle: "@" + account.Username,
		RewardTx:      rewardTx,
		Message:       "Verification successful, 1 USDC sent",
	}, nil
}

// extractTweetID extracts tweet ID from URL
func extractTweetID(tweetURL string) (string, error) {
	parsed, err := url.Parse(tweetURL)
	if err != nil {
		return "", err
	}

	// Path should be /{username}/status/{tweetID}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 3 || parts[1] != "status" {
		return "", fmt.Errorf("invalid tweet URL format")
	}

	return parts[2], nil
}
