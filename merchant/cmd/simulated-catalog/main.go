// Command simulated-catalog creates an isolated, deterministic local fixture
// containing multiple Merchant SQLite databases and synthetic products.
// The generated sellers have public-key-shaped identifiers only; no private
// keys or payment credentials are created.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/stablepay/merchant-server/config"
	"github.com/stablepay/merchant-server/internal/domain/entity"
	"github.com/stablepay/merchant-server/internal/infrastructure/persistence/sqlite"
)

const (
	defaultOutputDir = "../.local-run/simulated-merchants"
	manifestFile     = "manifest.json"
	maxMerchants     = 100
	maxTotalProducts = 1_000_000
)

var base58Alphabet = []byte("123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz")

type merchantSpec struct {
	ID           string   `json:"merchant_id"`
	Name         string   `json:"name"`
	Category     string   `json:"category"`
	Seller       string   `json:"seller_address"`
	MerchantDID  string   `json:"merchant_did"`
	Port         int      `json:"port"`
	ProductCount int      `json:"product_count"`
	ProductsURL  string   `json:"products_url"`
	Tags         []string `json:"tags"`
}

type manifest struct {
	SchemaVersion       int            `json:"schema_version"`
	Dataset             string         `json:"dataset"`
	Seed                int64          `json:"seed"`
	MerchantCount       int            `json:"merchant_count"`
	ProductCount        int            `json:"product_count"`
	ProductsPerMerchant int            `json:"products_per_merchant"`
	BasePort            int            `json:"base_port"`
	PurchaseEnabled     bool           `json:"purchase_enabled"`
	PaymentReady        bool           `json:"payment_ready"`
	Notes               []string       `json:"notes"`
	Merchants           []merchantSpec `json:"merchants"`
}

type categorySpec struct {
	name     string
	category string
	tags     []string
	items    []string
	formats  []string
	styles   []string
	details  []string
}

var categorySpecs = []categorySpec{
	{name: "晨光咖啡实验室", category: "coffee", tags: []string{"咖啡", "饮品", "烘焙"}, items: []string{"耶加雪菲", "曼特宁", "哥伦比亚", "危地马拉", "云南日晒", "巴西黄波旁"}, formats: []string{"挂耳装", "冷萃包", "拼配豆", "单品豆"}, styles: []string{"花果香", "坚果可可", "焦糖甜感", "清爽平衡"}, details: []string{"适合日常手冲与办公室分享", "按批次烘焙并提供风味说明", "包含产区和处理法的数字商品介绍"}},
	{name: "纸飞机数字书店", category: "books", tags: []string{"阅读", "电子书", "出版"}, items: []string{"城市漫游手记", "产品设计入门", "数据思维练习", "世界建筑图册", "创作者访谈集", "写作方法笔记"}, formats: []string{"电子版", "精读版", "注释版", "合集版"}, styles: []string{"入门精选", "专题资料", "年度更新", "典藏内容"}, details: []string{"提供可下载的数字阅读内容", "附带章节摘要与主题标签", "适合个人学习和资料整理"}},
	{name: "向量学习社", category: "education", tags: []string{"课程", "学习", "教育"}, items: []string{"Go 服务设计", "Agent 工具实践", "数据库基础", "分布式系统导读", "产品分析方法", "安全编码训练"}, formats: []string{"视频课", "练习包", "讲义包", "专题课"}, styles: []string{"基础路线", "项目实战", "短课精选", "进阶专题"}, details: []string{"包含课程说明和自学练习", "按主题拆分为可独立学习的单元", "提供数字讲义与学习路线"}},
	{name: "像素工坊素材店", category: "design-assets", tags: []string{"设计", "素材", "创意"}, items: []string{"移动端图标", "仪表盘组件", "插画角色", "界面线框", "品牌色板", "演示模板"}, formats: []string{"Figma 文件", "SVG 素材", "PNG 套件", "组件包"}, styles: []string{"极简风", "柔和渐变", "几何构成", "复古配色"}, details: []string{"适用于原型和演示项目", "包含授权说明与文件清单", "提供分层组织的数字素材"}},
	{name: "接口集市", category: "developer-tools", tags: []string{"API", "开发者", "软件"}, items: []string{"日志分析接口", "地址标准化接口", "文本摘要接口", "汇率演示接口", "图片压缩工具", "Webhook 调试包"}, formats: []string{"入门额度", "开发套餐", "月度包", "测试许可"}, styles: []string{"个人开发", "原型验证", "团队试用", "演示环境"}, details: []string{"用于本地开发和接口联调", "提供调用说明与示例请求", "模拟服务不连接生产数据"}},
	{name: "星图音频资料馆", category: "audio", tags: []string{"音频", "音乐", "创作"}, items: []string{"环境氛围音", "短片配乐", "节奏循环包", "播客片头", "钢琴练习曲", "自然采样集"}, formats: []string{"WAV 套装", "MP3 合集", "分轨包", "授权说明包"}, styles: []string{"轻爵士", "电子氛围", "原声质感", "自然现场"}, details: []string{"包含音频文件和基础元数据", "适用于个人创作样例", "附带模拟授权范围说明"}},
	{name: "远山影像素材", category: "stock-media", tags: []string{"影像", "图片", "素材"}, items: []string{"山野晨雾", "城市夜景", "植物微距", "海岸线条", "工作空间", "抽象纹理"}, formats: []string{"高清图片包", "短视频片段", "主题合集", "预览授权包"}, styles: []string{"自然光", "电影色调", "清新纪实", "极简构图"}, details: []string{"包含预览图和素材清单", "适用于原型与演示内容", "不包含真实人物个人信息"}},
	{name: "纸上造物模板铺", category: "templates", tags: []string{"模板", "效率", "办公"}, items: []string{"项目计划板", "会议纪要模板", "研究记录表", "作品集框架", "预算跟踪表", "内容日历"}, formats: []string{"Notion 模板", "表格模板", "文档套件", "演示稿模板"}, styles: []string{"清晰简约", "团队协作", "个人管理", "研究记录"}, details: []string{"提供可复制的模板结构", "包含填写示例和使用说明", "适用于学习与演示"}},
	{name: "青岚健康读物", category: "wellness-guides", tags: []string{"健康", "生活", "指南"}, items: []string{"睡眠习惯手册", "轻运动计划", "饮食记录指南", "呼吸练习卡", "户外步行路线", "日常放松练习"}, formats: []string{"电子手册", "练习卡包", "记录模板", "主题合集"}, styles: []string{"日常入门", "轻量实践", "一周计划", "自我记录"}, details: []string{"属于一般生活方式内容，不代替专业医疗建议", "提供自我记录与习惯养成材料", "附带内容范围和使用提示"}},
	{name: "方糖游戏收藏", category: "game-collectibles", tags: []string{"游戏", "收藏", "数字内容"}, items: []string{"像素徽章", "地图主题包", "角色头像集", "桌面壁纸", "任务卡片", "音乐纪念包"}, formats: []string{"数字收藏包", "主题套装", "头像授权包", "纪念合集"}, styles: []string{"像素风", "科幻风", "复古街机", "幻想手绘"}, details: []string{"用于演示的数字内容条目", "不代表链上 NFT 或游戏内资产", "包含模拟授权和下载说明"}},
	{name: "蓝鲸云端工具", category: "cloud-tools", tags: []string{"云工具", "效率", "软件"}, items: []string{"文件校验工具", "JSON 格式助手", "日程整理器", "图表导出器", "小型任务看板", "批量命名工具"}, formats: []string{"单次许可", "试用额度", "个人版", "工具合集"}, styles: []string{"轻量版", "开发版", "个人效率", "演示许可"}, details: []string{"本数据集只表示模拟商品目录", "不调用真实云服务或处理用户数据", "提供用途说明和演示边界"}},
	{name: "拾光旅行资料", category: "travel-guides", tags: []string{"旅行", "地图", "攻略"}, items: []string{"海滨步行指南", "古镇路线图", "博物馆清单", "城市咖啡地图", "亲子周末计划", "轻徒步准备表"}, formats: []string{"电子攻略", "离线清单", "地图标注包", "主题资料包"}, styles: []string{"周末轻旅行", "城市漫步", "自然探索", "亲子出行"}, details: []string{"为虚构地点和演示任务生成", "出行前应自行核对实时信息", "提供路线结构与行前清单"}},
}

func main() {
	merchants := flag.Int("merchants", 12, "number of simulated Merchant instances (1-100)")
	productsPerMerchant := flag.Int("products-per-merchant", 2000, "products generated for each Merchant")
	seed := flag.Int64("seed", 20261010, "deterministic product-content seed")
	basePort := flag.Int("base-port", 8781, "first local Merchant HTTP port")
	outputDir := flag.String("out-dir", defaultOutputDir, "new output directory; must not already exist")
	flag.Parse()

	if err := generate(*merchants, *productsPerMerchant, *seed, *basePort, *outputDir); err != nil {
		fmt.Fprintln(os.Stderr, "simulated-catalog:", err)
		os.Exit(1)
	}
}

func generate(merchantCount, productsPerMerchant int, seed int64, basePort int, outputDir string) error {
	if merchantCount < 1 || merchantCount > maxMerchants {
		return fmt.Errorf("merchants must be between 1 and %d", maxMerchants)
	}
	if productsPerMerchant < 1 || merchantCount*productsPerMerchant > maxTotalProducts {
		return fmt.Errorf("total products must be between 1 and %d", maxTotalProducts)
	}
	if basePort < 1024 || basePort+merchantCount-1 > 65535 {
		return errors.New("base-port range is invalid")
	}
	if merchantCount > len(categorySpecs) {
		return fmt.Errorf("merchant count exceeds configured category profiles (%d)", len(categorySpecs))
	}

	absoluteOutput, err := filepath.Abs(outputDir)
	if err != nil {
		return fmt.Errorf("resolve output directory: %w", err)
	}
	if _, err := os.Stat(absoluteOutput); err == nil {
		return fmt.Errorf("output path already exists; choose a new --out-dir to preserve existing data: %s", absoluteOutput)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("check output path: %w", err)
	}
	parent := filepath.Dir(absoluteOutput)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create output parent: %w", err)
	}
	stage, err := os.MkdirTemp(parent, ".simulated-merchants-stage-")
	if err != nil {
		return fmt.Errorf("create staging directory: %w", err)
	}
	completed := false
	defer func() {
		if !completed {
			fmt.Fprintf(os.Stderr, "partial generated files retained for inspection at %s\n", stage)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	data := manifest{
		SchemaVersion:       1,
		Dataset:             "stablepay-simulated-merchant-catalog",
		Seed:                seed,
		MerchantCount:       merchantCount,
		ProductCount:        merchantCount * productsPerMerchant,
		ProductsPerMerchant: productsPerMerchant,
		BasePort:            basePort,
		PurchaseEnabled:     false,
		PaymentReady:        false,
		Notes: []string{
			"Synthetic listing fixture only; no real Merchant, DID registration, wallet keys, payment, or Devnet settlement is created.",
			"Each Merchant has an isolated SQLite products table and a local HTTP listing endpoint.",
			"Commerce Runtime merchant_capabilities are a separate service-capability catalog and are not populated by this product dataset.",
			"Product records do not model stock or shipping inventory.",
		},
		Merchants: make([]merchantSpec, 0, merchantCount),
	}

	for merchantIndex := 0; merchantIndex < merchantCount; merchantIndex++ {
		spec := categorySpecs[merchantIndex]
		merchantID := fmt.Sprintf("sim-m%02d", merchantIndex+1)
		sellerAddress := syntheticPublicKey(merchantIndex + 1)
		port := basePort + merchantIndex
		merchantDir := filepath.Join(stage, merchantID)
		dataDir := filepath.Join(merchantDir, "data")
		if err := os.MkdirAll(dataDir, 0o755); err != nil {
			return fmt.Errorf("create data directory for %s: %w", merchantID, err)
		}
		profile := merchantSpec{
			ID:           merchantID,
			Name:         spec.name,
			Category:     spec.category,
			Seller:       sellerAddress,
			MerchantDID:  "did:solana:" + sellerAddress,
			Port:         port,
			ProductCount: productsPerMerchant,
			ProductsURL:  fmt.Sprintf("http://127.0.0.1:%d/api/v1/products", port),
			Tags:         append([]string{"synthetic", merchantID}, spec.tags...),
		}
		if err := writeMerchantConfig(merchantDir, profile); err != nil {
			return err
		}
		if err := writeJSON(filepath.Join(dataDir, "gift_codes.json"), map[string]any{"gift_codes": []any{}}); err != nil {
			return fmt.Errorf("write empty simulated gift-code pool: %w", err)
		}

		repo, err := sqlite.NewProductRepo(filepath.Join(dataDir, "merchant.db"), true)
		if err != nil {
			return fmt.Errorf("open SQLite for %s: %w", merchantID, err)
		}
		rng := rand.New(rand.NewSource(seed + int64(merchantIndex+1)*1_000_003))
		for productIndex := 1; productIndex <= productsPerMerchant; productIndex++ {
			product, buildErr := buildProduct(profile, spec, productIndex, rng)
			if buildErr != nil {
				_ = repo.Close()
				return fmt.Errorf("build %s product %d: %w", merchantID, productIndex, buildErr)
			}
			if err := repo.Save(ctx, product); err != nil {
				_ = repo.Close()
				return fmt.Errorf("save %s product %d: %w", merchantID, productIndex, err)
			}
		}
		_, total, err := repo.FindAll(ctx, 1, 1)
		closeErr := repo.Close()
		if err != nil {
			return fmt.Errorf("verify %s products: %w", merchantID, err)
		}
		if closeErr != nil {
			return fmt.Errorf("close %s SQLite: %w", merchantID, closeErr)
		}
		if total != int64(productsPerMerchant) {
			return fmt.Errorf("verify %s count: got %d, want %d", merchantID, total, productsPerMerchant)
		}
		data.Merchants = append(data.Merchants, profile)
		fmt.Printf("generated %s: %d active products, port %d\n", merchantID, total, port)
	}

	if err := writeJSON(filepath.Join(stage, manifestFile), data); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	readme := "# Simulated Merchant Catalog\n\n" +
		"Generated local fixture: " + fmt.Sprintf("%d simulated Merchant profiles, %d synthetic active products, seed %d.\n\n", data.MerchantCount, data.ProductCount, data.Seed) +
		"The fixture contains independent SQLite databases and generated per-merchant `config.yaml` files. It is for local product listing and pagination tests. Purchase is disabled as a dataset claim: no private keys, DID registration, funded wallets, payment verification, or Devnet operations are provided.\n\n" +
		"Start the instances from the repository root with `powershell -NoProfile -ExecutionPolicy Bypass -File .\\scripts\\start-simulated-merchants.ps1`; stop only those recorded instances with the same command and `-Stop`. The public listing endpoint accepts `page` and `size` (maximum 100), so 2,000 products require 20 pages per Merchant.\n\n" +
		"Commerce Runtime's `merchant_capabilities` table is a separate capability catalog and is not populated by these product rows. The current Product model has no inventory/stock field. See `manifest.json` for the full boundary.\n"
	if err := os.WriteFile(filepath.Join(stage, "README.md"), []byte(readme), 0o644); err != nil {
		return fmt.Errorf("write dataset README: %w", err)
	}
	if err := os.Rename(stage, absoluteOutput); err != nil {
		return fmt.Errorf("publish generated dataset: %w; staged output remains at %s", err, stage)
	}
	completed = true
	fmt.Printf("dataset complete: %d merchants / %d products\n", data.MerchantCount, data.ProductCount)
	fmt.Printf("output: %s\n", absoluteOutput)
	fmt.Printf("manifest: %s\n", filepath.Join(absoluteOutput, manifestFile))
	return nil
}

func buildProduct(profile merchantSpec, spec categorySpec, index int, rng *rand.Rand) (*entity.Product, error) {
	item := spec.items[rng.Intn(len(spec.items))]
	format := spec.formats[rng.Intn(len(spec.formats))]
	style := spec.styles[rng.Intn(len(spec.styles))]
	detail := spec.details[rng.Intn(len(spec.details))]
	priceCents := 99 + rng.Intn(19_901)
	price := fmt.Sprintf("%d.%02d", priceCents/100, priceCents%100)
	sku := fmt.Sprintf("%s-%06d", profile.ID, index)
	title := fmt.Sprintf("%s · %s · %s · %06d", item, format, style, index)
	description := fmt.Sprintf("%s，分类：%s；商家：%s。编号 %s。该记录由固定种子生成，仅用于本地目录展示和分页压测。", detail, spec.category, profile.Name, sku)
	tags := append([]string(nil), profile.Tags...)
	tags = append(tags, format, style, fmt.Sprintf("batch-%03d", (index-1)/100+1))
	imageURL := fmt.Sprintf("https://images.example.invalid/%s/%06d.jpg", profile.ID, index)
	return entity.NewProductBuilder().
		WithSKUID(sku).
		WithTitle(title).
		WithDescription(description).
		WithImageURL(imageURL).
		WithPrice(price, entity.CurrencyUSDC).
		WithAuthor(profile.Name).
		WithTags(tags).
		WithStatus(entity.ProductStatusActive).
		WithSellerAddress(profile.Seller).
		Build()
}

func writeMerchantConfig(merchantDir string, profile merchantSpec) error {
	proofHash := sha256.Sum256([]byte("simulation-only-proof-secret:" + profile.ID))
	cfg := config.Config{
		Server: config.ServerConfig{Host: "127.0.0.1", Port: profile.Port, Name: profile.ID},
		StablePay: config.StablePayConfig{
			GatewayBaseURL: "http://127.0.0.1:1",
			APIKey:         "SIMULATION_ONLY_NOT_A_SECRET",
			FacilitatorURL: "http://127.0.0.1:1",
		},
		Merchant: config.MerchantConfig{
			SellerAddress: profile.Seller,
			ProofSecret:   fmt.Sprintf("simulation-only-%x", proofHash[:12]),
			PublicBaseURL: fmt.Sprintf("http://127.0.0.1:%d", profile.Port),
		},
		Database: config.DatabaseConfig{
			Driver:      "sqlite",
			Path:        "./data/merchant.db",
			AutoMigrate: true,
			SeedEnabled: false,
		},
		Blockchain: config.BlockchainConfig{SolanaNetwork: "solana:devnet-simulation", USDCMint: "SIMULATED_USDC_MINT"},
	}
	encoded, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal %s config: %w", profile.ID, err)
	}
	if err := os.WriteFile(filepath.Join(merchantDir, "config.yaml"), encoded, 0o644); err != nil {
		return fmt.Errorf("write %s config: %w", profile.ID, err)
	}
	return nil
}

func syntheticPublicKey(index int) string {
	seed := sha256.Sum256([]byte(fmt.Sprintf("stablepay-simulated-merchant-public-key-%03d", index)))
	return base58Encode(seed[:])
}

func base58Encode(source []byte) string {
	value := new(big.Int).SetBytes(source)
	base := big.NewInt(58)
	zero := big.NewInt(0)
	var encoded []byte
	for value.Cmp(zero) > 0 {
		quotient, remainder := new(big.Int), new(big.Int)
		quotient.QuoRem(value, base, remainder)
		encoded = append(encoded, base58Alphabet[remainder.Int64()])
		value = quotient
	}
	for _, b := range source {
		if b != 0 {
			break
		}
		encoded = append(encoded, base58Alphabet[0])
	}
	for left, right := 0, len(encoded)-1; left < right; left, right = left+1, right-1 {
		encoded[left], encoded[right] = encoded[right], encoded[left]
	}
	return string(encoded)
}

func writeJSON(path string, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(encoded, '\n'), 0o644)
}

func init() {
	if len(categorySpecs) != 12 {
		panic("simulated catalog category profile count must remain exactly 12")
	}
	for i := range categorySpecs {
		categorySpecs[i].name = strings.TrimSpace(categorySpecs[i].name)
	}
}
