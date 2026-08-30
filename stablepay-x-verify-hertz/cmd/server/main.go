package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"stablepay-x-verify-hertz/internal/config"
	"stablepay-x-verify-hertz/internal/model"
	"stablepay-x-verify-hertz/internal/service"
	"stablepay-x-verify-hertz/internal/store"
	"stablepay-x-verify-hertz/internal/util"
)

type Server struct {
	verifySvc      *service.VerifyTwitterService
	xOAuthSvc      *service.XOAuthService
	realVerifySvc  *service.RealVerifyService
	didStore       *store.DIDStore
	tweetStore     *store.TweetStore
	bindingStore   *store.BindingStore
	rewardStore    *store.RewardStore
	xSessionStore  *store.XOAuthSessionStore
	xAccountStore  *store.XAccountStore
	xBindingStore  *store.XBindingStore
}

func main() {
	// Load configuration
	config.Load()

	s := &Server{
		didStore:      store.NewDIDStore(),
		tweetStore:    store.NewTweetStore(),
		bindingStore:  store.NewBindingStore(),
		rewardStore:   store.NewRewardStore(),
		xSessionStore: store.NewXOAuthSessionStore(),
		xAccountStore: store.NewXAccountStore(),
		xBindingStore: store.NewXBindingStore(),
	}
	s.verifySvc = service.NewVerifyTwitterService(s.didStore, s.tweetStore, s.bindingStore, s.rewardStore)
	s.xOAuthSvc = service.NewXOAuthService(s.xSessionStore, s.xAccountStore, s.xBindingStore, s.didStore)
	s.realVerifySvc = service.NewRealVerifyService(s.didStore, s.bindingStore, s.rewardStore, s.xBindingStore)

	mux := http.NewServeMux()

	// Static files (verify page)
	mux.HandleFunc("/verify", s.handleVerifyPage)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("./web"))))

	// API endpoints per minimal chain spec
	// DID endpoints
	mux.HandleFunc("/api/v1/did", s.handleCreateDID)

	// Real X OAuth endpoints (per minimal chain spec) - kept for backward compatibility
	mux.HandleFunc("/api/v1/x/oauth/start", s.handleXOAuthStart)
	mux.HandleFunc("/api/v1/x/oauth/callback", s.handleXOAuthCallback)
	mux.HandleFunc("/api/v1/x/oauth/status", s.handleXOAuthStatus)

	// Verification endpoints (MVP - using real X API read-only)
	mux.HandleFunc("/api/v1/verify-twitter", s.handleVerifyTwitter) // Real X API verify
	mux.HandleFunc("/api/v1/verify", s.handleGetVerifyStatus)       // Check verification status

	// Legacy mock endpoints (for development/testing)
	mux.HandleFunc("/api/v1/mock/did", s.handleMockDID)
	mux.HandleFunc("/api/v1/mock/twitter/tweets", s.handleMockTweet)
	mux.HandleFunc("/api/v1/mock/bindings", s.handleGetBindings)
	mux.HandleFunc("/api/v1/verify/twitter", s.handleMockVerify) // Legacy mock verify

	handler := corsMiddleware(mux)

	port := config.C.Port
	log.Printf("stablepay x verify listening on :%s", port)
	log.Printf("Configuration:")
	log.Printf("  - X API Base URL: %s", config.C.XAPIBaseURL)
	if config.C.XBearerToken != "" {
		log.Printf("  - Auth Method: Bearer Token (preferred)")
	} else if config.C.XConsumerKey != "" {
		log.Printf("  - Auth Method: OAuth 1.0a")
	} else {
		log.Printf("  - WARNING: No X API credentials configured")
	}
	log.Printf("  - Frontend URL: %s", config.C.FrontendVerifyURL)
	log.Printf("  - Verify page: http://localhost:%s/verify?did=...", port)
	log.Fatal(http.ListenAndServe(":"+port, handler))
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// handleVerifyPage serves the verify.html page with DID parameter
func (s *Server) handleVerifyPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Serve the verify.html file
	http.ServeFile(w, r, "./web/verify.html")
}

// handleCreateDID creates a new DID (per minimal chain spec)
func (s *Server) handleCreateDID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		DID           string `json:"did"`
		WalletAddress string `json:"wallet_address"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid json")
		return
	}

	if !util.IsValidDID(req.DID) {
		writeError(w, http.StatusBadRequest, "invalid_did", "did format invalid")
		return
	}

	didIdentity := model.DIDIdentity{
		DID:           req.DID,
		WalletAddress: req.WalletAddress,
		CreatedAt:     time.Now().UTC(),
	}
	s.didStore.Save(didIdentity)

	writeJSON(w, http.StatusOK, didIdentity)
}

// X OAuth Start - initiates OAuth flow (kept for backward compatibility)
func (s *Server) handleXOAuthStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	did := r.URL.Query().Get("did")
	if did == "" {
		writeError(w, http.StatusBadRequest, "missing_param", "did parameter required")
		return
	}

	resp, err := s.xOAuthSvc.StartOAuth(did)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), err.Error())
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// X OAuth Callback - handles OAuth callback from X (kept for backward compatibility)
func (s *Server) handleXOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	errorMsg := r.URL.Query().Get("error")

	if errorMsg != "" {
		// Handle OAuth error from X
		redirectURL := fmt.Sprintf("%s?oauth=failed&reason=%s",
			config.C.FrontendVerifyURL,
			url.QueryEscape(errorMsg),
		)
		http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
		return
	}

	if code == "" || state == "" {
		redirectURL := fmt.Sprintf("%s?oauth=failed&reason=missing_code_or_state",
			config.C.FrontendVerifyURL,
		)
		http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
		return
	}

	redirectURL, err := s.xOAuthSvc.HandleCallback(code, state)
	if err != nil {
		errorRedirect := fmt.Sprintf("%s?oauth=failed&reason=%s",
			config.C.FrontendVerifyURL,
			url.QueryEscape(err.Error()),
		)
		http.Redirect(w, r, errorRedirect, http.StatusTemporaryRedirect)
		return
	}

	http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
}

// X OAuth Status - checks if DID is connected to X (kept for backward compatibility)
func (s *Server) handleXOAuthStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	did := r.URL.Query().Get("did")
	if did == "" {
		writeError(w, http.StatusBadRequest, "missing_param", "did parameter required")
		return
	}

	status, err := s.xOAuthSvc.GetOAuthStatus(did)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, status)
}

// handleVerifyTwitter - real verification using X API (MVP version)
// This uses Bearer Token or OAuth 1.0a to read tweets directly, without OAuth callback flow
func (s *Server) handleVerifyTwitter(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		DID      string `json:"did"`
		TweetURL string `json:"tweet_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid json")
		return
	}

	// Check if X API is configured
	if config.C.XBearerToken == "" && config.C.XConsumerKey == "" {
		writeError(w, http.StatusServiceUnavailable, "x_api_not_configured",
			"X API not configured. Set X_BEARER_TOKEN or X_CONSUMER_KEY/X_ACCESS_TOKEN environment variables.")
		return
	}

	// Call real X API verification using read-only access
	resp, err := s.realVerifySvc.Verify(service.VerifyRequest{
		DID:      req.DID,
		TweetURL: req.TweetURL,
	})
	if err != nil {
		// Map error to appropriate error code
		errStr := err.Error()
		switch {
		case contains(errStr, "invalid_did"):
			writeError(w, http.StatusBadRequest, "invalid_did", "DID format is invalid")
		case contains(errStr, "did_not_found"):
			writeError(w, http.StatusBadRequest, "did_not_found", "DID not found, create wallet first")
		case contains(errStr, "invalid_tweet_url"):
			writeError(w, http.StatusBadRequest, "invalid_tweet_url", "Invalid tweet URL format")
		case contains(errStr, "tweet_not_found"):
			writeError(w, http.StatusBadRequest, "tweet_not_found", "Tweet not found or not accessible")
		case contains(errStr, "tweet_author_mismatch"):
			writeError(w, http.StatusBadRequest, "tweet_author_mismatch", "Tweet author does not match URL")
		case contains(errStr, "did_not_in_tweet"):
			writeError(w, http.StatusBadRequest, "did_not_in_tweet", "Tweet does not contain the DID")
		case contains(errStr, "invalid_tweet_content"):
			writeError(w, http.StatusBadRequest, "invalid_tweet_content", "Tweet does not contain required verification prefix")
		case contains(errStr, "twitter_account_protected"):
			writeError(w, http.StatusBadRequest, "twitter_account_protected", "Twitter account is protected (private)")
		case contains(errStr, "did_already_bound"):
			writeError(w, http.StatusBadRequest, "did_already_bound", "DID already bound to another Twitter account")
		case contains(errStr, "twitter_already_bound"):
			writeError(w, http.StatusBadRequest, "twitter_already_bound", "Twitter account already bound to another DID")
		case contains(errStr, "x_api_error"):
			writeError(w, http.StatusInternalServerError, "x_api_error", "X API request failed: "+errStr)
		default:
			writeError(w, http.StatusInternalServerError, "verification_failed", err.Error())
		}
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// handleGetVerifyStatus - checks verification status for a DID
func (s *Server) handleGetVerifyStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	did := r.URL.Query().Get("did")
	if did == "" {
		writeError(w, http.StatusBadRequest, "missing_param", "did parameter required")
		return
	}

	status, err := s.realVerifySvc.GetVerifyStatus(did)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), err.Error())
		return
	}

	writeJSON(w, http.StatusOK, status)
}

// Legacy mock endpoints (for development/testing)

func (s *Server) handleMockDID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		DID           string `json:"did"`
		WalletAddress string `json:"wallet_address"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid json")
		return
	}

	if !util.IsValidDID(req.DID) {
		writeError(w, http.StatusBadRequest, "invalid_did", "did format invalid")
		return
	}

	s.didStore.Save(model.DIDIdentity{
		DID:           req.DID,
		WalletAddress: req.WalletAddress,
		CreatedAt:     time.Now().UTC(),
	})

	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (s *Server) handleMockTweet(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, []model.MockTweet{})
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req model.MockCreateTweetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid json")
		return
	}

	handle, _, normalizedURL, err := util.ParseTweetURL(req.TweetURL)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_url", err.Error())
		return
	}

	tweet := model.MockTweet{
		TweetURL:     normalizedURL,
		AuthorHandle: handle,
		Text:         req.Text,
		IsPublic:     req.IsPublic,
		CreatedAt:    time.Now().UTC(),
	}

	s.tweetStore.Save(tweet)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"tweet":   tweet,
	})
}

func (s *Server) handleGetBindings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	did := r.URL.Query().Get("did")
	if did != "" {
		binding, exists := s.bindingStore.GetByDID(did)
		if !exists {
			writeError(w, http.StatusNotFound, "not_found", "binding not found")
			return
		}
		writeJSON(w, http.StatusOK, binding)
		return
	}

	bindings := s.bindingStore.List()
	writeJSON(w, http.StatusOK, bindings)
}

func (s *Server) handleMockVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req model.VerifyTwitterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid json")
		return
	}

	resp, errResp := s.verifySvc.Verify(req)
	if errResp != nil {
		writeError(w, http.StatusBadRequest, errResp.Code, errResp.Message)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, model.ErrorResponse{
		Success: false,
		Code:    code,
		Message: message,
	})
}
