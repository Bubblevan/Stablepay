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
	AgentDid      string    `gorm:"index"`
	WalletAddress string    `gorm:"index"`
	TweetUrl      string
	TweetContent  string
	Verified      bool      `gorm:"default:false"`
	RewardAmount  int64     // 最小单位，如 100000 = 0.1 USDC (6 decimals)
	RewardTxId    string
	VerifiedAt    time.Time
}