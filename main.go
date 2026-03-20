package main

import (
	"log"
	"net"

	"github.com/cloudwego/kitex/server"
	verification_service "verification-service/kitex_gen/stablepay/verification_service/verificationservice"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var DB *gorm.DB

func main() {
	// 连接数据库
	var err error
	DB, err = gorm.Open(sqlite.Open("test.db"), &gorm.Config{})
	if err != nil {
		log.Fatalf("数据库连接失败: %v", err)
	}

	// 让数据库按照 PurchaseRecord 这个结构体建表
	DB.AutoMigrate(&PurchaseRecord{})

	go StartMQConsumer()

	// 监听 8085 端口
	addr, _ := net.ResolveTCPAddr("tcp", "localhost:8085")
	svr := verification_service.NewServer(new(VerificationServiceImpl), server.WithServiceAddr(addr))

	err = svr.Run()
	if err != nil {
		log.Println(err.Error())
	}
}

// 表单的定义
type PurchaseRecord struct {
	gorm.Model
	AgentDid string `gorm:"index"`
	SkillDid string `gorm:"index"`
	TxId     string
}
