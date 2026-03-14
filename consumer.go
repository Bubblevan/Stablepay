package main

import (
	"context"
	"encoding/json"
	"log"

	"github.com/apache/rocketmq-client-go/v2"
	"github.com/apache/rocketmq-client-go/v2/consumer"
	"github.com/apache/rocketmq-client-go/v2/primitive"
)

type PaymentSuccessEvent struct {
	AgentDid string `json:"agent_did"`
	SkillDid string `json:"skill_did"`
	TxId     string `json:"tx_id"`
}//定义了支付服务发来的 JSON 消息

func StartMQConsumer() {
	// 创建消费者，RocketMQ 地址留空 (这里先用本地默认地址 127.0.0.1:9876 占位)
	c, err := rocketmq.NewPushConsumer(
		consumer.WithGroupName("verification_group"),
		consumer.WithNameServer([]string{"127.0.0.1:9876"}), 
	)
	if err != nil {
		log.Printf("⚠️ 创建 MQ 消费者失败: %v", err)
		return
	}

	// 订阅 "payment_events" 这个主题 (Topic)
	err = c.Subscribe("payment_events", consumer.MessageSelector{}, func(ctx context.Context,
		msgs ...*primitive.MessageExt) (consumer.ConsumeResult, error) {

		for _, msg := range msgs {
			log.Printf("📥 收到支付消息: %s", msg.Body)

			// 把收到的 JSON 字符串解析成 Go 的结构体
			var event PaymentSuccessEvent
			if err := json.Unmarshal(msg.Body, &event); err != nil {
				log.Printf("解析消息失败: %v", err)
				continue 
			}

			// 存入数据库
			record := PurchaseRecord{
				AgentDid: event.AgentDid,
				SkillDid: event.SkillDid,
				TxId:     event.TxId,
			}
			
			// 调用 main.go 里的全局 DB 变量写入数据
			if err := DB.Create(&record).Error; err != nil {
				log.Printf("写入数据库失败: %v", err)
				return consumer.ConsumeRetryLater, err // 告诉 MQ写入失败，等待重发
			}

			log.Printf("成功消费支付事件，购买关系已入库！流水号: %s", event.TxId)
		}
		
		return consumer.ConsumeSuccess, nil
	})

	if err != nil {
		log.Printf("订阅 MQ 失败: %v", err)
		return
	}

	// 启动监听
	err = c.Start()
	if err != nil {
		log.Printf("启动 MQ 消费者失败: %v", err)
		return
	}
	log.Println("🎧 RocketMQ 消费者启动成功，正在后台监听支付消息...")
}