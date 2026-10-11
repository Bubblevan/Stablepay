package composition

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/internal/catalog"
	"github.com/stablepay/commerce-runtime/internal/repository"
)

const (
	merchantCatalogManifestVersion = "stablepay.merchant-capability-manifest.v1"
	merchantCatalogPageSize        = 100
	merchantCatalogMaxProducts     = 10000
	merchantCatalogBodyLimit       = 2 << 20
)

var merchantSourceIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

var (
	ErrInvalidMerchantCatalogManifest = errors.New("invalid Merchant capability manifest")
	ErrMerchantCatalogTooLarge        = errors.New("Merchant catalog exceeds the configured product bound")
	ErrMerchantCatalogProductMissing  = errors.New("manifest SKU is not present in the active Merchant catalog")
)

// MerchantCatalogManifest is operator-authored trusted metadata. Merchant's
// ordinary product DTO supplies only display fields; task schemas, supported
// protocols, settlement currencies, Merchant identity, and payee identity must
// be declared here rather than guessed from descriptions or tags.
type MerchantCatalogManifest struct {
	SchemaVersion string                  `json:"schema_version"`
	Sources       []MerchantCatalogSource `json:"sources"`
}

type MerchantCatalogSource struct {
	SourceID      string                      `json:"source_id"`
	MerchantDID   string                      `json:"merchant_did"`
	PayeeDID      string                      `json:"payee_did"`
	ProductAPIURL string                      `json:"product_api_url"`
	Capabilities  []MerchantProductCapability `json:"capabilities"`
}

type MerchantProductCapability struct {
	SKUID                     string   `json:"sku_id"`
	CapabilityID              string   `json:"capability_id"`
	MetadataVersion           int64    `json:"metadata_version"`
	AllowMissingSKU           bool     `json:"allow_missing_sku,omitempty"`
	TaskTypes                 []string `json:"task_types"`
	SemanticTags              []string `json:"semantic_tags,omitempty"`
	InputSchemaRef            string   `json:"input_schema_ref"`
	OutputSchemaRef           string   `json:"output_schema_ref"`
	InputContentTypes         []string `json:"input_content_types"`
	OutputContentTypes        []string `json:"output_content_types"`
	SupportedProtocolVersions []string `json:"supported_protocol_versions"`
	SupportedCurrencies       []string `json:"supported_currencies"`
	PricingModel              string   `json:"pricing_model"`
}

type MerchantCatalogReader interface {
	ReadCapabilities(context.Context) ([]*catalog.MerchantCapability, error)
}

type HTTPMerchantCatalogReader struct {
	client   *http.Client
	manifest MerchantCatalogManifest
}

func LoadMerchantCatalogManifest(path string) (MerchantCatalogManifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return MerchantCatalogManifest{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, merchantCatalogBodyLimit+1))
	decoder.DisallowUnknownFields()
	var manifest MerchantCatalogManifest
	if err := decoder.Decode(&manifest); err != nil {
		return MerchantCatalogManifest{}, fmt.Errorf("decode Merchant capability manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return MerchantCatalogManifest{}, fmt.Errorf("decode Merchant capability manifest: trailing data")
	}
	if err := manifest.Validate(); err != nil {
		return MerchantCatalogManifest{}, err
	}
	return manifest, nil
}

func (m MerchantCatalogManifest) Validate() error {
	if m.SchemaVersion != merchantCatalogManifestVersion || len(m.Sources) == 0 {
		return ErrInvalidMerchantCatalogManifest
	}
	sourceIDs := make(map[string]struct{}, len(m.Sources))
	merchantDIDs := make(map[string]struct{}, len(m.Sources))
	for _, source := range m.Sources {
		source.SourceID = strings.TrimSpace(source.SourceID)
		source.MerchantDID = strings.TrimSpace(source.MerchantDID)
		source.PayeeDID = strings.TrimSpace(source.PayeeDID)
		productURL, err := url.Parse(strings.TrimSpace(source.ProductAPIURL))
		if !merchantSourceIDPattern.MatchString(source.SourceID) || source.MerchantDID == "" || source.PayeeDID == "" || err != nil ||
			productURL.Host == "" || productURL.User != nil || productURL.RawQuery != "" || productURL.Fragment != "" ||
			(productURL.Scheme != "http" && productURL.Scheme != "https") {
			return ErrInvalidMerchantCatalogManifest
		}
		if _, exists := sourceIDs[source.SourceID]; exists {
			return ErrInvalidMerchantCatalogManifest
		}
		if _, exists := merchantDIDs[source.MerchantDID]; exists {
			return ErrInvalidMerchantCatalogManifest
		}
		sourceIDs[source.SourceID] = struct{}{}
		merchantDIDs[source.MerchantDID] = struct{}{}
		seenSKU := make(map[string]struct{}, len(source.Capabilities))
		seenCapability := make(map[string]struct{}, len(source.Capabilities))
		for _, metadata := range source.Capabilities {
			if strings.TrimSpace(metadata.SKUID) == "" || strings.TrimSpace(metadata.SKUID) != metadata.SKUID ||
				strings.ContainsAny(metadata.SKUID, "/\\") || metadata.SKUID == "." || metadata.SKUID == ".." ||
				strings.TrimSpace(metadata.CapabilityID) == "" || strings.TrimSpace(metadata.CapabilityID) != metadata.CapabilityID ||
				metadata.MetadataVersion <= 0 || metadata.MetadataVersion > 9999999999 ||
				strings.TrimSpace(metadata.InputSchemaRef) == "" || strings.TrimSpace(metadata.OutputSchemaRef) == "" ||
				len(metadata.TaskTypes) == 0 || len(metadata.InputContentTypes) == 0 || len(metadata.OutputContentTypes) == 0 ||
				len(metadata.SupportedProtocolVersions) == 0 || len(metadata.SupportedCurrencies) == 0 || strings.TrimSpace(metadata.PricingModel) == "" {
				return ErrInvalidMerchantCatalogManifest
			}
			if _, exists := seenSKU[metadata.SKUID]; exists {
				return ErrInvalidMerchantCatalogManifest
			}
			capabilityKey := strings.ToLower(strings.TrimSpace(metadata.CapabilityID))
			if _, exists := seenCapability[capabilityKey]; exists {
				return ErrInvalidMerchantCatalogManifest
			}
			seenSKU[metadata.SKUID] = struct{}{}
			seenCapability[capabilityKey] = struct{}{}
		}
	}
	return nil
}

func NewHTTPMerchantCatalogReader(client *http.Client, manifest MerchantCatalogManifest) (*HTTPMerchantCatalogReader, error) {
	if err := manifest.Validate(); err != nil {
		return nil, err
	}
	if client == nil {
		client = &http.Client{}
	}
	copy := *client
	copy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return &HTTPMerchantCatalogReader{client: &copy, manifest: manifest}, nil
}

func (r *HTTPMerchantCatalogReader) ReadCapabilities(ctx context.Context) ([]*catalog.MerchantCapability, error) {
	if r == nil || r.client == nil {
		return nil, ErrInvalidMerchantCatalogManifest
	}
	result := make([]*catalog.MerchantCapability, 0)
	for _, source := range r.manifest.Sources {
		if len(source.Capabilities) == 0 {
			continue
		}
		products, err := r.readActiveProducts(ctx, source.ProductAPIURL)
		if err != nil {
			return nil, fmt.Errorf("read Merchant source %s: %w", source.SourceID, err)
		}
		productBySKU := make(map[string]merchantCatalogProduct, len(products))
		for _, product := range products {
			if _, exists := productBySKU[product.SKUID]; exists {
				return nil, fmt.Errorf("Merchant source %s returned a duplicate SKU", source.SourceID)
			}
			productBySKU[product.SKUID] = product
		}
		metadata := append([]MerchantProductCapability(nil), source.Capabilities...)
		sort.Slice(metadata, func(i, j int) bool { return metadata[i].SKUID < metadata[j].SKUID })
		for _, declaration := range metadata {
			product, ok := productBySKU[strings.TrimSpace(declaration.SKUID)]
			if !ok {
				var found bool
				product, found, err = r.readProductDetail(ctx, source.ProductAPIURL, declaration.SKUID)
				if err != nil {
					return nil, fmt.Errorf("read Merchant detail %s/%s: %w", source.SourceID, declaration.SKUID, err)
				}
				if !found && declaration.AllowMissingSKU {
					continue
				}
				if !found {
					return nil, fmt.Errorf("%w: source=%s sku=%s (set allow_missing_sku only when retiring a deleted SKU)", ErrMerchantCatalogProductMissing, source.SourceID, declaration.SKUID)
				}
				if strings.EqualFold(product.Status, "active") {
					return nil, fmt.Errorf("%w: active SKU is missing from paged product list: source=%s sku=%s", ErrMerchantCatalogProductMissing, source.SourceID, declaration.SKUID)
				}
			}
			capability, err := capabilityFromMerchantProduct(source, declaration, product)
			if err != nil {
				return nil, fmt.Errorf("map Merchant source %s SKU %s: %w", source.SourceID, declaration.SKUID, err)
			}
			result = append(result, capability)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].MerchantDID != result[j].MerchantDID {
			return result[i].MerchantDID < result[j].MerchantDID
		}
		return result[i].CapabilityID < result[j].CapabilityID
	})
	return result, nil
}

type merchantCatalogProduct struct {
	SKUID       string   `json:"sku_id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Price       string   `json:"price"`
	Currency    string   `json:"currency"`
	Status      string   `json:"status"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
	Tags        []string `json:"tags"`
}

type merchantCatalogPage struct {
	OK         bool                     `json:"ok"`
	Products   []merchantCatalogProduct `json:"products"`
	Pagination struct {
		Total int64 `json:"total"`
		Page  int   `json:"page"`
		Size  int   `json:"size"`
	} `json:"pagination"`
}

func (r *HTTPMerchantCatalogReader) readActiveProducts(ctx context.Context, productAPIURL string) ([]merchantCatalogProduct, error) {
	base, err := url.Parse(productAPIURL)
	if err != nil {
		return nil, ErrInvalidMerchantCatalogManifest
	}
	products := make([]merchantCatalogProduct, 0)
	expectedTotal := int64(-1)
	for page := 1; ; page++ {
		requestURL := *base
		query := requestURL.Query()
		query.Set("page", strconv.Itoa(page))
		query.Set("size", strconv.Itoa(merchantCatalogPageSize))
		requestURL.RawQuery = query.Encode()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
		if err != nil {
			return nil, err
		}
		request.Header.Set("Accept", "application/json")
		request.Header.Set("User-Agent", "stablepay-commerce-runtime/1")
		response, err := r.client.Do(request)
		if err != nil {
			return nil, errors.New("Merchant catalog request failed")
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, merchantCatalogBodyLimit+1))
		_ = response.Body.Close()
		if readErr != nil || len(body) > merchantCatalogBodyLimit {
			return nil, errors.New("Merchant catalog response is unreadable or oversized")
		}
		if response.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("Merchant catalog returned HTTP %d", response.StatusCode)
		}
		var result merchantCatalogPage
		if err := json.Unmarshal(body, &result); err != nil || !result.OK {
			return nil, errors.New("Merchant catalog returned an invalid page")
		}
		if expectedTotal < 0 {
			expectedTotal = result.Pagination.Total
			if expectedTotal < 0 || expectedTotal > merchantCatalogMaxProducts {
				return nil, ErrMerchantCatalogTooLarge
			}
		}
		if result.Pagination.Total != expectedTotal || result.Pagination.Page != page || result.Pagination.Size != merchantCatalogPageSize || len(result.Products) > merchantCatalogPageSize {
			return nil, errors.New("Merchant catalog pagination changed during read")
		}
		products = append(products, result.Products...)
		if len(products) > merchantCatalogMaxProducts || int64(len(products)) > expectedTotal {
			return nil, ErrMerchantCatalogTooLarge
		}
		if int64(len(products)) == expectedTotal {
			break
		}
		if len(result.Products) == 0 {
			return nil, errors.New("Merchant catalog ended before the advertised total")
		}
	}
	return products, nil
}

func (r *HTTPMerchantCatalogReader) readProductDetail(ctx context.Context, productAPIURL, skuID string) (merchantCatalogProduct, bool, error) {
	base, err := url.Parse(productAPIURL)
	if err != nil {
		return merchantCatalogProduct{}, false, ErrInvalidMerchantCatalogManifest
	}
	skuID = strings.TrimSpace(skuID)
	if skuID == "" || strings.ContainsAny(skuID, "/\\") || skuID == "." || skuID == ".." {
		return merchantCatalogProduct{}, false, ErrInvalidMerchantCatalogManifest
	}
	basePath := strings.TrimRight(base.Path, "/")
	baseEscapedPath := strings.TrimRight(base.EscapedPath(), "/")
	base.Path = basePath + "/" + skuID
	base.RawPath = baseEscapedPath + "/" + url.PathEscape(skuID)
	base.RawQuery = ""
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return merchantCatalogProduct{}, false, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "stablepay-commerce-runtime/1")
	response, err := r.client.Do(request)
	if err != nil {
		return merchantCatalogProduct{}, false, errors.New("Merchant product detail request failed")
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, merchantCatalogBodyLimit+1))
	_ = response.Body.Close()
	if readErr != nil || len(body) > merchantCatalogBodyLimit {
		return merchantCatalogProduct{}, false, errors.New("Merchant product detail is unreadable or oversized")
	}
	if response.StatusCode == http.StatusNotFound {
		return merchantCatalogProduct{}, false, nil
	}
	if response.StatusCode != http.StatusOK {
		return merchantCatalogProduct{}, false, fmt.Errorf("Merchant product detail returned HTTP %d", response.StatusCode)
	}
	var result struct {
		OK      bool                    `json:"ok"`
		Product *merchantCatalogProduct `json:"product"`
	}
	if err := json.Unmarshal(body, &result); err != nil || !result.OK || result.Product == nil {
		return merchantCatalogProduct{}, false, errors.New("Merchant product detail returned an invalid payload")
	}
	return *result.Product, true, nil
}

func capabilityFromMerchantProduct(source MerchantCatalogSource, metadata MerchantProductCapability, product merchantCatalogProduct) (*catalog.MerchantCapability, error) {
	skuID := strings.TrimSpace(product.SKUID)
	if skuID == "" || skuID != strings.TrimSpace(metadata.SKUID) || strings.TrimSpace(product.Title) == "" || strings.TrimSpace(product.Description) == "" {
		return nil, errors.New("Merchant product identity or description is invalid")
	}
	status := catalog.StatusInactive
	availability := catalog.AvailabilityUnavailable
	if strings.EqualFold(strings.TrimSpace(product.Status), "active") {
		status = catalog.StatusActive
		availability = catalog.AvailabilityAvailable
	} else if !strings.EqualFold(strings.TrimSpace(product.Status), "inactive") && !strings.EqualFold(strings.TrimSpace(product.Status), "draft") {
		return nil, errors.New("Merchant product status is unsupported")
	}
	currency := strings.ToUpper(strings.TrimSpace(product.Currency))
	if !containsString(metadata.SupportedCurrencies, currency) {
		return nil, errors.New("Merchant product currency is not declared by the capability")
	}
	priceMinor, err := parseBusinessMinor(product.Price)
	if err != nil || priceMinor <= 0 {
		return nil, errors.New("Merchant product price is not representable in minor units")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(product.CreatedAt))
	if err != nil {
		return nil, errors.New("Merchant product created_at is invalid")
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(product.UpdatedAt))
	if err != nil || updatedAt.Before(createdAt) {
		return nil, errors.New("Merchant product updated_at is invalid")
	}
	apiURL, _ := url.Parse(source.ProductAPIURL)
	basePath := strings.TrimRight(apiURL.Path, "/")
	endpoint := *apiURL
	endpoint.Path = basePath + "/" + skuID + "/execute"
	baseEscapedPath := strings.TrimRight(apiURL.EscapedPath(), "/")
	endpoint.RawPath = baseEscapedPath + "/" + url.PathEscape(skuID) + "/execute"
	endpoint.RawQuery = ""
	endpoint.Fragment = ""

	price := priceMinor
	capability := &catalog.MerchantCapability{
		MerchantDID: source.MerchantDID, CapabilityID: metadata.CapabilityID, ProductID: skuID, PayeeDID: source.PayeeDID,
		Name: product.Title, Description: product.Description, TaskTypes: append([]string(nil), metadata.TaskTypes...),
		SemanticTags:   append([]string(nil), metadata.SemanticTags...),
		InvokeEndpoint: catalog.EndpointRef{Endpoint: endpoint.String(), Method: http.MethodGet},
		InputSchemaRef: metadata.InputSchemaRef, OutputSchemaRef: metadata.OutputSchemaRef,
		InputContentTypes: append([]string(nil), metadata.InputContentTypes...), OutputContentTypes: append([]string(nil), metadata.OutputContentTypes...),
		SupportedProtocolVersions: append([]string(nil), metadata.SupportedProtocolVersions...), SupportedCurrencies: append([]string(nil), metadata.SupportedCurrencies...),
		PricingModel: metadata.PricingModel, PriceHintMinor: &price, PriceHintCurrency: currency,
		Status: status, Availability: availability,
		CatalogVersion: fmt.Sprintf("v%013d-r%010d", updatedAt.UTC().UnixMilli(), metadata.MetadataVersion),
		Source:         "merchant-http", SourceRef: "merchant-catalog://" + source.SourceID + "/" + url.PathEscape(skuID),
		ValidFrom:  createdAt.UTC(),
		ValidUntil: time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC), CreatedAt: createdAt.UTC(), UpdatedAt: updatedAt.UTC(),
	}
	if err := refreshMerchantCapabilitySourceHash(capability); err != nil {
		return nil, err
	}
	return capability, nil
}

func parseBusinessMinor(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "+-eE") {
		return 0, errors.New("invalid decimal")
	}
	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" {
		return 0, errors.New("invalid decimal")
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
	}
	if len(fraction) > 2 {
		return 0, errors.New("too many fractional digits")
	}
	for _, part := range parts {
		for _, char := range part {
			if char < '0' || char > '9' {
				return 0, errors.New("invalid decimal digit")
			}
		}
	}
	fraction = fraction + strings.Repeat("0", 2-len(fraction))
	whole, ok := new(big.Int).SetString(parts[0], 10)
	if !ok {
		return 0, errors.New("invalid whole amount")
	}
	whole.Mul(whole, big.NewInt(100))
	if fraction != "" {
		fractionValue, ok := new(big.Int).SetString(fraction, 10)
		if !ok {
			return 0, errors.New("invalid fractional amount")
		}
		whole.Add(whole, fractionValue)
	}
	if !whole.IsInt64() {
		return 0, errors.New("amount overflows minor units")
	}
	return whole.Int64(), nil
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), expected) {
			return true
		}
	}
	return false
}

func SyncMerchantCatalog(ctx context.Context, store repository.CatalogRepository, reader MerchantCatalogReader, manifest MerchantCatalogManifest) error {
	if store == nil || reader == nil || manifest.Validate() != nil {
		return ErrInvalidMerchantCatalogManifest
	}
	capabilities, err := reader.ReadCapabilities(ctx)
	if err != nil {
		return err
	}
	newByKey := make(map[string]*catalog.MerchantCapability, len(capabilities))
	for _, capability := range capabilities {
		if capability == nil {
			return errors.New("Merchant catalog reader returned a nil capability")
		}
		key := capability.MerchantDID + "\x00" + capability.CapabilityID
		if _, exists := newByKey[key]; exists {
			return fmt.Errorf("Merchant catalog maps multiple products to the same capability: %s", capability.CapabilityID)
		}
		current, currentErr := store.GetCurrentActiveCapability(ctx, capability.MerchantDID, capability.CapabilityID)
		if currentErr != nil && !errors.Is(currentErr, repository.ErrNotFound) {
			return fmt.Errorf("load current Merchant capability %s/%s: %w", capability.MerchantDID, capability.CapabilityID, currentErr)
		}
		if currentErr == nil && catalog.CompareVersions(capability.CatalogVersion, current.CatalogVersion) <= 0 {
			if sameMerchantCapabilityContent(capability, current) {
				newByKey[key] = capability
				continue
			}
			// Merchant timestamps can have second-level precision. If facts
			// change inside that window, allocate a newer immutable Runtime
			// version rather than failing startup on a version collision.
			capability.CatalogVersion = nextMerchantCatalogVersion(current.CatalogVersion, time.Now().UTC().UnixMilli())
			if err := refreshMerchantCapabilitySourceHash(capability); err != nil {
				return fmt.Errorf("rehash changed Merchant capability %s/%s: %w", capability.MerchantDID, capability.CapabilityID, err)
			}
		}
		newByKey[key] = capability
		if err := store.SaveCapabilityVersion(ctx, capability); err != nil {
			return fmt.Errorf("persist Merchant capability %s/%s: %w", capability.MerchantDID, capability.CapabilityID, err)
		}
	}
	current, err := store.ListActiveCapabilities(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Round(time.Millisecond)
	for _, previous := range current {
		if previous.Source != "merchant-http" || !strings.HasPrefix(previous.SourceRef, "merchant-catalog://") {
			continue
		}
		key := previous.MerchantDID + "\x00" + previous.CapabilityID
		if _, remainsActive := newByKey[key]; remainsActive {
			continue
		}
		retired := previous.Clone()
		retired.Status = catalog.StatusInactive
		retired.Availability = catalog.AvailabilityUnavailable
		retired.ValidFrom = now
		retired.ValidUntil = time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)
		retired.CreatedAt = now
		retired.UpdatedAt = now
		retiredDigest := sha256.Sum256([]byte("retired:" + previous.SourceHash + ":" + now.Format(time.RFC3339Nano)))
		retired.SourceHash = hex.EncodeToString(retiredDigest[:])
		retired.CatalogVersion = nextMerchantCatalogVersion(previous.CatalogVersion, now.UnixMilli())
		if err := store.SaveCapabilityVersion(ctx, retired); err != nil {
			return fmt.Errorf("retire missing Merchant capability %s/%s: %w", retired.MerchantDID, retired.CapabilityID, err)
		}
	}
	return nil
}

func refreshMerchantCapabilitySourceHash(capability *catalog.MerchantCapability) error {
	capability.SourceHash = ""
	canonical, err := capability.CanonicalSnapshot()
	if err != nil {
		return err
	}
	digest := sha256.Sum256(canonical)
	capability.SourceHash = hex.EncodeToString(digest[:])
	return capability.Validate()
}

func sameMerchantCapabilityContent(left, right *catalog.MerchantCapability) bool {
	normalizeForCompare := func(capability *catalog.MerchantCapability) *catalog.MerchantCapability {
		copy := capability.Clone()
		copy.CatalogVersion = "comparison"
		copy.SourceHash = ""
		copy.ValidFrom = copy.CreatedAt
		copy.ValidUntil = time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)
		copy.UpdatedAt = copy.CreatedAt
		return copy
	}
	leftSnapshot, leftErr := normalizeForCompare(left).CanonicalSnapshot()
	rightSnapshot, rightErr := normalizeForCompare(right).CanonicalSnapshot()
	return leftErr == nil && rightErr == nil && string(leftSnapshot) == string(rightSnapshot)
}

func nextMerchantCatalogVersion(previous string, nowMillis int64) string {
	const prefix = "v"
	version := strings.TrimPrefix(previous, prefix)
	parts := strings.SplitN(version, "-r", 2)
	previousMillis, _ := strconv.ParseInt(parts[0], 10, 64)
	previousRevision := int64(0)
	if len(parts) == 2 {
		previousRevision, _ = strconv.ParseInt(parts[1], 10, 64)
	}
	if nowMillis > previousMillis {
		return fmt.Sprintf("v%013d-r%010d", nowMillis, 1)
	}
	return fmt.Sprintf("v%013d-r%010d", previousMillis, previousRevision+1)
}
