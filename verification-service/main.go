package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"github.com/cloudwego/kitex/server"
	verification_service "verification-service/kitex_gen/stablepay/verification_service/verificationservice"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

var DB *gorm.DB

func main() {
	// 连接数据库
	var err error
	DB, err = openMySQL()
	if err != nil {
		log.Fatalf("数据库连接失败: %v", err)
	}

	// 根据结构体自动建表
	if err := DB.AutoMigrate(&PurchaseRecord{}, &XVerification{}); err != nil {
		log.Fatalf("自动迁移表结构失败: %v", err)
	}

	// 初始化 X API 客户端
	initXAPIClient(
		envOrDefault("X_API_KEY", ""),
		envOrDefault("X_API_SECRET", ""),
		envOrDefault("X_ACCESS_TOKEN", ""),
		envOrDefault("X_ACCESS_TOKEN_SECRET", ""),
	)

	go StartMQConsumer()

	// 监听 8085 端口（0.0.0.0 以便容器内其他服务通过服务名访问）
	addr, _ := net.ResolveTCPAddr("tcp", "0.0.0.0:8085")
	svr := verification_service.NewServer(new(VerificationServiceImpl), server.WithServiceAddr(addr))

	err = svr.Run()
	if err != nil {
		log.Println(err.Error())
	}
}

// envOrDefault 获取环境变量，若不存在则返回默认值
func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// openMySQL 打开 MySQL 数据库连接
func openMySQL() (*gorm.DB, error) {
	host := envOrDefault("MYSQL_HOST", "stablepay-mysql")
	port := envOrDefault("MYSQL_PORT", "3306")
	user := envOrDefault("MYSQL_USER", "stablepay")
	password := envOrDefault("MYSQL_PASSWORD", "stablepay123")
	dbName := envOrDefault("MYSQL_DBNAME", "stablepay_verification_db")

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		user, password, host, port, dbName)
	return gorm.Open(mysql.Open(dsn), &gorm.Config{})
}

// PurchaseRecord 购买记录表
type PurchaseRecord struct {
	gorm.Model
	AgentDid string `gorm:"type:varchar(128);uniqueIndex:uk_agent_skill"`
	SkillDid string `gorm:"type:varchar(128);uniqueIndex:uk_agent_skill"`
	TxId     string `gorm:"type:varchar(64);index"`
}

// XVerification X 平台验证记录表
type XVerification struct {
	gorm.Model
	// AgentDid 唯一索引:防止并发请求导致同一 agent 多次领奖。
	// 配合 VerifyXTweet 的 unique 冲突检测,真正由 DB 兜底而不是"先查后插"的竞态。
	// 注:AutoMigrate 不会把现有 non-unique index 升级为 unique。
	// 如果生产表已有重复 agent_did 行,需要先清理再 ALTER TABLE 添加 unique 约束。
	AgentDid      string    `gorm:"uniqueIndex;column:agent_did;size:128"`
	WalletAddress string    `gorm:"index"`
	XUsername     string    `gorm:"index"` // X 账号用户名，用于防止重复绑定
	TweetUrl      string
	TweetContent  string
	Verified      bool      `gorm:"default:false"`
	RewardAmount  int64     // 最小单位，例如 1000000 = 1 USDC (6 位小数)
	RewardTxId    string    `gorm:"type:varchar(64)"`
	VerifiedAt    time.Time
}
