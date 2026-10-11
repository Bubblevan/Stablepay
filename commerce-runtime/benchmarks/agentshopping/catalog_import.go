package agentshopping

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

type merchantManifest struct {
	MerchantID   string `json:"merchant_id"`
	Name         string `json:"name"`
	Category     string `json:"category"`
	Port         int    `json:"port"`
	ProductCount int    `json:"product_count"`
	ProductsURL  string `json:"products_url"`
}

type sourceManifest struct {
	Seed          int64              `json:"seed"`
	MerchantCount int                `json:"merchant_count"`
	ProductCount  int                `json:"product_count"`
	Merchants     []merchantManifest `json:"merchants"`
}

type sourceProductPage struct {
	Products []struct {
		SKUID       string   `json:"sku_id"`
		Title       string   `json:"title"`
		Description string   `json:"description"`
		ImageURL    string   `json:"image_url"`
		Price       string   `json:"price"`
		Currency    string   `json:"currency"`
		Tags        []string `json:"tags"`
		Status      string   `json:"status"`
	} `json:"products"`
	Pagination struct {
		Total int64 `json:"total"`
		Page  int   `json:"page"`
		Size  int   `json:"size"`
	} `json:"pagination"`
}

type ImportedCatalog struct {
	Seed          int64
	ManifestPath  string
	Merchants     []MerchantRecord
	Products      []Product
	ProductRows   []ProductRecord
	Inventory     []Inventory
	InventoryRows []InventoryRecord
}

func LoadMerchantCatalog(ctx context.Context, manifestPath string, client *http.Client) (ImportedCatalog, error) {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return ImportedCatalog{}, fmt.Errorf("read simulated Merchant manifest: %w", err)
	}
	var manifest sourceManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return ImportedCatalog{}, fmt.Errorf("parse simulated Merchant manifest: %w", err)
	}
	if manifest.MerchantCount != 12 || manifest.ProductCount != 24000 || len(manifest.Merchants) != 12 {
		return ImportedCatalog{}, fmt.Errorf("source manifest must describe 12 merchants and 24,000 products; got merchants=%d products=%d profiles=%d", manifest.MerchantCount, manifest.ProductCount, len(manifest.Merchants))
	}

	catalog := ImportedCatalog{Seed: manifest.Seed, ManifestPath: manifestPath,
		Merchants: make([]MerchantRecord, 0, 12), Products: make([]Product, 0, 24000),
		ProductRows: make([]ProductRecord, 0, 24000), Inventory: make([]Inventory, 0, 96000),
		InventoryRows: make([]InventoryRecord, 0, 96000)}
	productByKey := make(map[string]Product, 24000)
	for _, merchant := range manifest.Merchants {
		if merchant.MerchantID == "" || merchant.Name == "" || merchant.Category == "" || merchant.ProductsURL == "" || merchant.ProductCount != 2000 {
			return ImportedCatalog{}, fmt.Errorf("invalid source merchant profile %q", merchant.MerchantID)
		}
		endpoint, err := url.Parse(merchant.ProductsURL)
		if err != nil || endpoint.Scheme != "http" || endpoint.Hostname() != "127.0.0.1" {
			return ImportedCatalog{}, fmt.Errorf("merchant %s source must be loopback HTTP", merchant.MerchantID)
		}
		profile := MerchantRecord{TenantID: SimulationTenantID, MerchantID: merchant.MerchantID, Name: merchant.Name,
			Category: merchant.Category, DID: "did:sim:merchant:" + merchant.MerchantID, DataOrigin: syntheticOrigin, CreatedAt: time.Now().UTC()}
		catalog.Merchants = append(catalog.Merchants, profile)
		pageSize := 100
		var expectedTotal int64 = -1
		for page := 1; ; page++ {
			requestURL, err := url.Parse(merchant.ProductsURL)
			if err != nil {
				return ImportedCatalog{}, err
			}
			query := requestURL.Query()
			query.Set("page", strconv.Itoa(page))
			query.Set("size", strconv.Itoa(pageSize))
			requestURL.RawQuery = query.Encode()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
			if err != nil {
				return ImportedCatalog{}, err
			}
			response, err := client.Do(req)
			if err != nil {
				return ImportedCatalog{}, fmt.Errorf("read %s page %d: %w (start the isolated local Merchant fixtures first)", merchant.MerchantID, page, err)
			}
			var result sourceProductPage
			decodeErr := json.NewDecoder(response.Body).Decode(&result)
			_ = response.Body.Close()
			if decodeErr != nil {
				return ImportedCatalog{}, fmt.Errorf("decode %s page %d: %w", merchant.MerchantID, page, decodeErr)
			}
			if response.StatusCode != http.StatusOK {
				return ImportedCatalog{}, fmt.Errorf("Merchant %s returned HTTP %d on page %d", merchant.MerchantID, response.StatusCode, page)
			}
			if expectedTotal < 0 {
				expectedTotal = result.Pagination.Total
			}
			if result.Pagination.Total != expectedTotal || expectedTotal != int64(merchant.ProductCount) || result.Pagination.Page != page || len(result.Products) > pageSize {
				return ImportedCatalog{}, fmt.Errorf("Merchant %s pagination mismatch on page %d: total=%d page=%d rows=%d", merchant.MerchantID, page, result.Pagination.Total, result.Pagination.Page, len(result.Products))
			}
			for _, source := range result.Products {
				if source.Status != "active" || source.Currency != "USDC" || !strings.HasPrefix(source.SKUID, merchant.MerchantID+"-") {
					return ImportedCatalog{}, fmt.Errorf("invalid active product source %s/%s", merchant.MerchantID, source.SKUID)
				}
				item, format, style, err := ParseProductTitle(source.Title)
				if err != nil {
					return ImportedCatalog{}, err
				}
				price, err := ParseMinorUnits(source.Price)
				if err != nil || price <= 0 {
					return ImportedCatalog{}, fmt.Errorf("invalid price for %s/%s", merchant.MerchantID, source.SKUID)
				}
				product := Product{ID: StableID("product", merchant.MerchantID, source.SKUID), SKUID: source.SKUID,
					MerchantID: merchant.MerchantID, Category: merchant.Category, Title: source.Title,
					Description: source.Description, ImageURL: source.ImageURL, Price: source.Price,
					PriceMinor: price, Currency: source.Currency, Tags: append([]string(nil), source.Tags...),
					Status: source.Status, Item: item, Format: format, Style: style}
				applyBenchmarkSubstitutionMetadata(&product)
				attributesJSON, err := attachSyntheticAttributes(&product, manifest.Seed)
				if err != nil {
					return ImportedCatalog{}, fmt.Errorf("build synthetic attributes for %s/%s: %w", merchant.MerchantID, source.SKUID, err)
				}
				catalog.Products = append(catalog.Products, product)
				productByKey[merchant.MerchantID+"\x00"+source.SKUID] = product
				tagsJSON, _ := json.Marshal(product.Tags)
				catalog.ProductRows = append(catalog.ProductRows, ProductRecord{TenantID: SimulationTenantID, SKUID: product.SKUID,
					MerchantID: product.MerchantID, Category: product.Category, Item: product.Item, Format: product.Format,
					Style: product.Style, Title: product.Title, Description: product.Description, ImageURL: product.ImageURL,
					BasePrice: product.PriceMinor, Currency: product.Currency, Status: product.Status, TagsJSON: tagsJSON,
					AttributesJSON: attributesJSON, DataOrigin: syntheticOrigin, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})
			}
			if len(result.Products) == 0 || int64(page*pageSize) >= expectedTotal {
				break
			}
		}
		if int64(len(catalog.Products)) == 0 {
			return ImportedCatalog{}, fmt.Errorf("Merchant %s returned no products", merchant.MerchantID)
		}
		merchantProducts := 0
		for _, product := range catalog.Products {
			if product.MerchantID == merchant.MerchantID {
				merchantProducts++
			}
		}
		if merchantProducts != merchant.ProductCount {
			return ImportedCatalog{}, fmt.Errorf("Merchant %s yielded %d products; want %d", merchant.MerchantID, merchantProducts, merchant.ProductCount)
		}
	}
	if len(productByKey) != 24000 || len(catalog.Products) != 24000 {
		return ImportedCatalog{}, fmt.Errorf("catalog SKU uniqueness/count mismatch: unique=%d products=%d", len(productByKey), len(catalog.Products))
	}
	for _, overlay := range MakeOverlay(catalog.Products, manifest.Seed) {
		product := productByKey[overlay.MerchantID+"\x00"+overlay.SKUID]
		catalog.Inventory = append(catalog.Inventory, Inventory{TenantID: SimulationTenantID,
			MerchantID: overlay.MerchantID, SKUID: overlay.SKUID, Region: overlay.Region,
			OnHand: overlay.OnHand, Eligible: overlay.Eligible, PriceMinor: overlay.PriceMinor, Revision: overlay.Revision})
		catalog.InventoryRows = append(catalog.InventoryRows, InventoryRecord{TenantID: SimulationTenantID,
			MerchantID: overlay.MerchantID, SKUID: overlay.SKUID, Region: overlay.Region,
			OnHand: overlay.OnHand, Reserved: 0, Eligible: overlay.Eligible, PriceMinor: overlay.PriceMinor,
			Currency: product.Currency, Revision: overlay.Revision, DataOrigin: syntheticOrigin})
	}
	sort.Slice(catalog.Merchants, func(i, j int) bool { return catalog.Merchants[i].MerchantID < catalog.Merchants[j].MerchantID })
	return catalog, nil
}

func BuildSyntheticAttributes(product Product, seed int64) map[string]any {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%d|%s|attributes-v1", seed, product.SKUID)))
	choice := func(offset int, values ...string) string { return values[int(digest[offset%len(digest)])%len(values)] }
	count := func(offset int, low, high int) int { return low + int(digest[offset%len(digest)])%(high-low+1) }
	attributes := map[string]any{
		"data_origin":      syntheticOrigin,
		"synthetic_notice": "Fictional catalog attributes generated for isolated commerce simulation; verify before any real-world use.",
	}
	switch product.Category {
	case "coffee":
		attributes["origin"] = coffeeOriginForItem(product.Item)
		attributes["net_weight_g"] = count(1, 100, 500)
		attributes["roast_level"] = choice(2, "light", "medium", "medium-dark")
		attributes["grind_options"] = coffeeGrindOptions(product.Format)
		attributes["tasting_notes"] = []string{product.Style, choice(3, "citrus", "cocoa", "stone fruit", "caramel")}
		attributes["best_before_days"] = count(4, 90, 270)
	case "books":
		attributes["language"] = choice(0, "zh-CN", "en-US", "zh-CN")
		attributes["page_count"] = count(1, 96, 480)
		attributes["publication_year"] = count(2, 2021, 2026)
		attributes["file_formats"] = []string{"PDF", "EPUB"}
		attributes["license"] = "personal-use-demo"
	case "education":
		attributes["duration_minutes"] = count(0, 45, 720)
		attributes["module_count"] = count(1, 4, 36)
		attributes["difficulty"] = choice(2, "beginner", "intermediate", "advanced")
		attributes["captions"] = []string{"zh-CN", "en-US"}
		attributes["access_days"] = count(3, 30, 365)
	case "design-assets":
		attributes["file_formats"] = designAssetFormats(product.Format)
		attributes["color_profiles"] = []string{"sRGB"}
		attributes["asset_count"] = count(0, 24, 480)
		attributes["license"] = choice(1, "personal-demo", "team-demo")
	case "developer-tools":
		attributes["api_version"] = choice(0, "2025-10", "2026-04", "2026-09")
		attributes["monthly_quota"] = count(1, 1000, 100000)
		attributes["rate_limit_rpm"] = count(2, 30, 600)
		attributes["support_regions"] = []string{"CN-EAST", "CN-NORTH", "CN-SOUTH", "CN-WEST"}
	case "audio":
		attributes["duration_seconds"] = count(0, 15, 900)
		attributes["sample_rate_hz"] = choice(1, "44100", "48000", "96000")
		attributes["bit_depth"] = choice(2, "16", "24")
		attributes["file_formats"] = audioFormats(product.Format)
		attributes["license"] = "demo-creator-use"
	case "stock-media":
		attributes["resolution"] = choice(0, "1920x1080", "3840x2160", "6000x4000")
		attributes["aspect_ratio"] = choice(1, "16:9", "3:2", "1:1")
		attributes["file_formats"] = stockMediaFormats(product.Format)
		attributes["license"] = "demo-project-only"
	case "templates":
		attributes["compatible_apps"] = []string{"Notion", "Markdown", "CSV"}
		attributes["page_count"] = count(0, 3, 42)
		attributes["update_cadence"] = choice(1, "quarterly", "annual", "one-time")
		attributes["license"] = "personal-use-demo"
	case "wellness-guides":
		attributes["page_count"] = count(0, 8, 96)
		attributes["content_type"] = wellnessContentType(product.Item)
		attributes["medical_disclaimer"] = "Not medical advice; fictional simulation content."
	case "game-collectibles":
		attributes["asset_type"] = gameCollectibleType(product.Item)
		attributes["resolution"] = choice(1, "1024x1024", "2048x2048", "3840x2160")
		attributes["license"] = "non-transferable-demo-license; not an NFT or game entitlement"
	case "cloud-tools":
		attributes["deployment"] = choice(0, "local-demo", "sandbox-only", "offline-tool")
		attributes["quota_units"] = count(1, 100, 20000)
		attributes["data_residency"] = "simulation-only"
	case "travel-guides":
		attributes["destination_code"] = travelDestinationCode(product.Item)
		attributes["page_count"] = count(1, 6, 64)
		attributes["last_reviewed"] = choice(2, "2025-10-01", "2026-04-01", "2026-09-01")
		attributes["travel_disclaimer"] = "Fictional demo guide; check official sources before travel."
	default:
		attributes["specification"] = map[string]any{"item": product.Item, "format": product.Format, "style": product.Style}
	}
	if group := curatedSubstitutionGroup(product.Category, product.Item, product.Format); group != "" {
		attributes["substitution_group"] = group
		attributes["substitution_policy"] = "benchmark-curated-v1; buyer must explicitly reconfirm"
	}
	return attributes
}

func coffeeOriginForItem(item string) string {
	switch item {
	case "耶加雪菲":
		return "Ethiopia / Yirgacheffe"
	case "曼特宁":
		return "Indonesia / Sumatra"
	case "哥伦比亚":
		return "Colombia / Huila"
	case "危地马拉":
		return "Guatemala / Huehuetenango"
	case "云南日晒":
		return "Yunnan / Baoshan"
	case "巴西黄波旁":
		return "Brazil / Cerrado"
	default:
		return "unknown / synthetic origin"
	}
}

func coffeeGrindOptions(format string) []string {
	switch format {
	case "挂耳装":
		return []string{"filter"}
	case "冷萃包":
		return []string{"coarse-ground"}
	default:
		return []string{"whole bean", "filter", "espresso"}
	}
}

func designAssetFormats(format string) []string {
	switch format {
	case "Figma 文件":
		return []string{"FIG"}
	case "SVG 素材":
		return []string{"SVG"}
	case "PNG 套件":
		return []string{"PNG"}
	default:
		return []string{"FIG", "SVG", "PNG"}
	}
}

func audioFormats(format string) []string {
	switch format {
	case "WAV 套装":
		return []string{"WAV"}
	case "MP3 合集":
		return []string{"MP3"}
	default:
		return []string{"WAV", "MP3"}
	}
}

func stockMediaFormats(format string) []string {
	switch format {
	case "短视频片段":
		return []string{"MP4"}
	case "高清图片包":
		return []string{"JPG", "PNG"}
	default:
		return []string{"JPG", "PNG", "MP4"}
	}
}

func wellnessContentType(item string) string {
	switch item {
	case "轻运动计划", "户外步行路线":
		return "light exercise guide"
	case "饮食记录指南":
		return "habit tracking"
	default:
		return "general wellness education"
	}
}

func gameCollectibleType(item string) string {
	switch item {
	case "角色头像集":
		return "avatar"
	case "桌面壁纸":
		return "wallpaper"
	case "音乐纪念包":
		return "sound pack"
	case "像素徽章":
		return "badge artwork"
	default:
		return "digital artwork"
	}
}

func travelDestinationCode(item string) string {
	switch item {
	case "海滨步行指南", "亲子周末计划":
		return "CN-DEMO-001"
	case "古镇路线图", "城市咖啡地图":
		return "CN-DEMO-002"
	default:
		return "CN-DEMO-003"
	}
}

func attachSyntheticAttributes(product *Product, seed int64) ([]byte, error) {
	if product == nil {
		return nil, fmt.Errorf("cannot attach synthetic attributes to a nil product")
	}
	attributes := BuildSyntheticAttributes(*product, seed)
	encoded, err := json.Marshal(attributes)
	if err != nil {
		return nil, err
	}
	product.Attributes = attributes
	return encoded, nil
}
