package config

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	// X API Credentials - OAuth 1.0a
	XConsumerKey       string
	XConsumerSecret    string
	XAccessToken       string
	XAccessTokenSecret string

	// X API Credentials - Bearer Token (preferred for read-only)
	XBearerToken string

	// X API Configuration
	XAPIBaseURL         string
	XVerifyTweetPrefix  string
	XVerifyTweetTemplate string

	// Legacy OAuth 2.0 (kept for backward compatibility but not used in MVP)
	XClientID           string
	XClientSecret       string
	XRedirectURI        string
	XOAuthScopes        string
	XAuthorizeURL       string
	XTokenURL           string

	// Server
	Port              string
	FrontendVerifyURL string
	EncryptionKey     string
}

var C *Config

func Load() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	C = &Config{
		// OAuth 1.0a credentials
		XConsumerKey:       getEnv("X_CONSUMER_KEY", ""),
		XConsumerSecret:    getEnv("X_CONSUMER_SECRET", ""),
		XAccessToken:       getEnv("X_ACCESS_TOKEN", ""),
		XAccessTokenSecret: getEnv("X_ACCESS_TOKEN_SECRET", ""),

		// Bearer Token (preferred)
		XBearerToken: getEnv("X_BEARER_TOKEN", ""),

		// X API Configuration
		XAPIBaseURL:         getEnv("X_API_BASE_URL", "https://api.x.com/2"),
		XVerifyTweetPrefix:  getEnv("X_VERIFY_TWEET_PREFIX", "I'm verifying my StablePay DID:"),
		XVerifyTweetTemplate: getEnv("X_VERIFY_TWEET_TEMPLATE", "I'm verifying my StablePay DID: {DID}"),

		// Legacy OAuth 2.0 (kept for backward compatibility)
		XClientID:           getEnv("X_CLIENT_ID", ""),
		XClientSecret:       getEnv("X_CLIENT_SECRET", ""),
		XRedirectURI:        getEnv("X_REDIRECT_URI", "http://localhost:8080/api/v1/x/oauth/callback"),
		XOAuthScopes:        getEnv("X_OAUTH_SCOPES", "tweet.read users.read offline.access"),
		XAuthorizeURL:       getEnv("X_AUTHORIZE_URL", "https://x.com/i/oauth2/authorize"),
		XTokenURL:           getEnv("X_TOKEN_URL", "https://api.x.com/2/oauth2/token"),

		// Server
		Port:              getEnv("PORT", "8080"),
		FrontendVerifyURL: getEnv("FRONTEND_VERIFY_URL", "http://127.0.0.1:3000/verify"),
		EncryptionKey:     getEnv("ENCRYPTION_KEY", ""),
	}

	// Check if we have at least one authentication method configured
	if C.XBearerToken == "" && (C.XConsumerKey == "" || C.XAccessToken == "") {
		log.Println("WARNING: No X API credentials configured. Set X_BEARER_TOKEN or X_CONSUMER_KEY/X_ACCESS_TOKEN environment variables.")
		log.Println("  - X_BEARER_TOKEN is preferred for read-only access to public data")
		log.Println("  - X_CONSUMER_KEY + X_ACCESS_TOKEN can be used as fallback (OAuth 1.0a)")
	}

	if C.EncryptionKey == "" {
		log.Println("WARNING: ENCRYPTION_KEY not set. Using fallback (INSECURE for production).")
		C.EncryptionKey = "your-32-byte-encryption-key-here!!"
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
