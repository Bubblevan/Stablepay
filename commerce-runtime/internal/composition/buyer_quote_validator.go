package composition

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/internal/application"
	"github.com/stablepay/commerce-runtime/internal/x402"
)

var (
	ErrLiveBuyerQuoteUnavailable = application.ErrBuyerQuoteValidationUnavailable
	ErrLiveBuyerQuoteChanged     = errors.New("current merchant quote differs from the buyer-approved quote")
)

// HTTPBuyerQuoteValidator re-reads the selected Merchant's existing x402
// resource. It is read-only: only a current HTTP 402 challenge with the same
// canonical terms may pass; redirects, success responses, and changed terms
// fail closed. Runtime remains the owner of approval and payment execution.
type HTTPBuyerQuoteValidator struct {
	Client    *http.Client
	Timeout   time.Duration
	BodyLimit int64
}

func NewHTTPBuyerQuoteValidator(client *http.Client) *HTTPBuyerQuoteValidator {
	if client == nil {
		client = &http.Client{}
	}
	return &HTTPBuyerQuoteValidator{Client: client, Timeout: 20 * time.Second, BodyLimit: x402.MaxChallengeBytes}
}

func (v *HTTPBuyerQuoteValidator) ValidateBuyerOffer(ctx context.Context, offer application.BuyerOffer) error {
	if v == nil || strings.TrimSpace(offer.RequesterDID) == "" || strings.TrimSpace(offer.QuoteHash) == "" || strings.TrimSpace(offer.ResourceURL) == "" {
		return ErrLiveBuyerQuoteUnavailable
	}
	resource, err := url.Parse(strings.TrimSpace(offer.ResourceURL))
	if err != nil || resource.Host == "" || resource.User != nil || (resource.Scheme != "http" && resource.Scheme != "https") {
		return ErrLiveBuyerQuoteUnavailable
	}
	query := resource.Query()
	query.Set("agent_did", offer.RequesterDID)
	resource.RawQuery = query.Encode()

	timeout := v.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, resource.String(), nil)
	if err != nil {
		return ErrLiveBuyerQuoteUnavailable
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "stablepay-commerce-runtime/1")

	client := v.Client
	if client == nil {
		client = &http.Client{}
	}
	clientCopy := *client
	clientCopy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	response, err := clientCopy.Do(req)
	if err != nil {
		return fmt.Errorf("%w: merchant request failed", ErrLiveBuyerQuoteUnavailable)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusPaymentRequired {
		return fmt.Errorf("%w: HTTP %d", ErrLiveBuyerQuoteUnavailable, response.StatusCode)
	}

	limit := v.BodyLimit
	if limit <= 0 || limit > x402.MaxChallengeBytes {
		limit = x402.MaxChallengeBytes
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(body)) > limit {
		return ErrLiveBuyerQuoteUnavailable
	}
	current, err := parseCurrentPaymentRequirement(response.Header, body)
	if err != nil {
		return fmt.Errorf("%w: invalid x402 challenge", ErrLiveBuyerQuoteUnavailable)
	}
	if current.CanonicalHash != offer.QuoteHash || current.BusinessAmountMinor != offer.AmountMinor ||
		!strings.EqualFold(current.Currency, offer.Currency) || !x402.EqualResourceURL(current.ResourceURL, offer.ResourceURL) ||
		current.ProductID != offer.ProductID || !strings.EqualFold(current.Network, offer.Network) ||
		!strings.EqualFold(current.Asset, offer.Asset) {
		return ErrLiveBuyerQuoteChanged
	}
	return nil
}

func parseCurrentPaymentRequirement(headers http.Header, body []byte) (x402.ParsedRequirement, error) {
	var firstValid *x402.ParsedRequirement
	for _, value := range headers.Values("PAYMENT-REQUIRED") {
		parsed, err := x402.ParseRequired(map[string]string{"PAYMENT-REQUIRED": value}, nil)
		if err != nil {
			continue
		}
		if parsed.ProtocolVersion == "x402-v2" {
			return parsed, nil
		}
		if firstValid == nil {
			copy := parsed
			firstValid = &copy
		}
	}
	if firstValid != nil {
		return *firstValid, nil
	}
	return x402.DecodeBodyRequirement(body)
}
