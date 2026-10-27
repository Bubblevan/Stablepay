package fiat

import (
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"gorm.io/gorm"
)

func FromEnv(store Store) (*Service, string, error) {
	token := os.Getenv("FIAT_INTERNAL_TOKEN")
	if len(token) < 24 {
		return nil, "", errors.New("FIAT_INTERNAL_TOKEN must contain at least 24 characters")
	}
	mode := os.Getenv("FIAT_PAYMENT_MODE")
	if mode == "" {
		mode = "local"
	}
	var provider Provider = LocalProvider{}
	switch mode {
	case "local":
	case "alipay_sandbox":
		private, err := os.ReadFile(os.Getenv("ALIPAY_PRIVATE_KEY_FILE"))
		if err != nil {
			return nil, "", errors.New("cannot read Alipay application private key file")
		}
		public, err := os.ReadFile(os.Getenv("ALIPAY_PUBLIC_KEY_FILE"))
		if err != nil {
			return nil, "", errors.New("cannot read Alipay public key file")
		}
		provider, err = NewAlipay(AlipayConfig{AppID: os.Getenv("ALIPAY_APP_ID"), SellerID: os.Getenv("ALIPAY_SELLER_ID"), NotifyURL: os.Getenv("ALIPAY_NOTIFY_URL"), ReturnURL: os.Getenv("ALIPAY_RETURN_URL")}, private, public)
		if err != nil {
			return nil, "", err
		}
	default:
		return nil, "", errors.New("unsupported FIAT_PAYMENT_MODE; use local or alipay_sandbox")
	}
	return NewService(store, provider), token, nil
}

// StartWithMySQL is opt-in on the existing Kitex service process.
func StartWithMySQL(db *gorm.DB, migrate bool) (*http.Server, error) {
	address := os.Getenv("FIAT_HTTP_ADDR")
	if address == "" {
		return nil, nil
	}
	store, err := NewSQLStore(db, migrate)
	if err != nil {
		return nil, err
	}
	service, token, err := FromEnv(store)
	if err != nil {
		return nil, err
	}
	return Listen(address, service, token)
}
func Listen(address string, service *Service, token string) (*http.Server, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	server := &http.Server{Addr: address, Handler: Handler(service, token), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("fiat HTTP server stopped: %v", err)
		}
	}()
	return server, nil
}
