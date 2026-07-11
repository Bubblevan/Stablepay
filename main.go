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

	// 让数据库按照结构体建表
	DB.AutoMigrate(&PurchaseRecord{}, &XVerification{})

	// 初始化 X API 客户端
	initXAPIClient(
		envOrDefault("X_API_KEY", ""),
		envOrDefault("X_API_SECRET", ""),
		envOrDefault("X_ACCESS_TOKEN", ""),
		envOrDefault("X_ACCESS_TOKEN_SECRET", ""),
	)

	go StartMQConsumer()

	// 监听 8085（0.0.0.0 以便容器内其它服务通过服务名访问）
	addr, _ := net.ResolveTCPAddr("tcp", "0.0.0.0:8085")
	svr := verification_service.NewServer(new(VerificationServiceImpl), server.WithServiceAddr(addr))

	err = svr.Run()
	if err != nil {
		log.Println(err.Error())
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

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

// 表单的定义
type PurchaseRecord struct {
	gorm.Model
	AgentDid string `gorm:"index"`
	SkillDid string `gorm:"index"`
	TxId     string
}

// X 验证表
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
	RewardAmount  int64     // 最小单位，如 1000000 = 1 USDC (6 decimals)
	RewardTxId    string
	VerifiedAt    time.Time
}