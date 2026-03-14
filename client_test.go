package main

import (
	"context"
	"log"
	"testing"

	"demo1/kitex_gen/stablepay/verification_service"
	"demo1/kitex_gen/stablepay/verification_service/verificationservice"
	"github.com/cloudwego/kitex/client"
	
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"	// 数据库依赖包
)

func TestVerifyPurchase(t *testing.T) {
	// 模拟 RocketMQ 消费者：
	db, err := gorm.Open(sqlite.Open("test.db"), &gorm.Config{})
	if err != nil {
		log.Fatal("连不上数据库:", err)
	}

	
	db.AutoMigrate(&PurchaseRecord{})// 强制测试脚本也去建表，不这样会报错

	testRecord := PurchaseRecord{
		AgentDid: "did:solana:qingfeng123", // 注意！要和下面查询的保持一致
		SkillDid: "did:solana:skill456",
		TxId:     "real_tx_999888",         // 真实流水号
	}
	db.Create(&testRecord) // 写入数据库
	log.Println("成功往 test.db 写入了一笔购买记录！")


	// 创建客户端，连接到本地 8888 端口
	c, err := verificationservice.NewClient("verification-service", client.WithHostPorts("127.0.0.1:8888"))
	if err != nil {
		log.Fatal(err)
	}

	// 构造请求参数
	req := &verification_service.VerifyPurchaseRequest{
		AgentDid: "did:solana:qingfeng123",
		SkillDid: "did:solana:skill456",
	}

	// 发起调用
	resp, err := c.VerifyPurchase(context.Background(), req)
	if err != nil {
		log.Fatal(err)
	}

	// 安全打印
	if resp.Purchased {
		log.Printf("🎉 Response: Purchased=%v, Time=%v, TxId=%v, Msg=%s", 
			resp.Purchased, *resp.PurchaseTime, *resp.TxId, resp.Base.Message)
	} else {
		log.Printf("Response: Purchased=%v, Msg=%s", 
			resp.Purchased, resp.Base.Message)
	}
}