package main

import (
	"log"
	verification_service "demo1/kitex_gen/stablepay/verification_service/verificationservice"

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
	// (理论上PurchaseRecord 结构体可以写在 main.go 里，也可以写在单独的 model.go 里)
	DB.AutoMigrate(&PurchaseRecord{})

	go StartMQConsumer()

	svr := verification_service.NewServer(new(VerificationServiceImpl))// 用来启动 Kitex 服务

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
