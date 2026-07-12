package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"os"
	"strings"

	"github.com/apache/rocketmq-client-go/v2"
	"github.com/apache/rocketmq-client-go/v2/consumer"
	"github.com/apache/rocketmq-client-go/v2/primitive"
	"gorm.io/gorm"
)

// PaymentSuccessEvent 定义支付成功事件的 JSON 结构
type PaymentSuccessEvent struct {
	AgentDid string `json:"agent_did"`
	SkillDid string `json:"skill_did"`
	TxId     string `json:"tx_id"`
}

// mqNameServers 获取 RocketMQ NameServer 地址列表
// 优先读取环境变量 ROCKETMQ_NAMESERVER，支持逗号分隔多个地址
// 若无配置则返回默认值 "127.0.0.1:9876"
func mqNameServers() []string {
	raw := strings.TrimSpace(os.Getenv("ROCKETMQ_NAMESERVER"))
	if raw == "" {
		return []string{"127.0.0.1:9876"}
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return []string{"127.0.0.1:9876"}
	}
	resolved := make([]string, 0, len(out))
	for _, addr := range out {
		resolved = append(resolved, resolveMQNameServerAddr(addr))
	}
	return resolved
}

// resolveMQNameServerAddr 解析 RocketMQ NameServer 地址
// 由于 rocketmq-client-go 在 K8s 环境下需要 IP:port 形式，该函数对 DNS 名称进行解析
func resolveMQNameServerAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		if !strings.Contains(addr, ":") {
			addr = net.JoinHostPort(addr, "9876")
		}
		host, port, err = net.SplitHostPort(addr)
		if err != nil {
			return addr
		}
	}
	ips, err := net.LookupHost(host)
	if err != nil || len(ips) == 0 {
		log.Printf("RocketMQ NameServer DNS 解析失败 %s: %v", addr, err)
		return addr
	}
	for _, ip := range ips {
		if net.ParseIP(ip).To4() != nil {
			resolved := net.JoinHostPort(ip, port)
			log.Printf("RocketMQ NameServer %s -> %s", addr, resolved)
			return resolved
		}
	}
	resolved := net.JoinHostPort(ips[0], port)
	log.Printf("RocketMQ NameServer %s -> %s", addr, resolved)
	return resolved
}

// StartMQConsumer 启动 RocketMQ 消费者，监听 payment_events 主题
// 收到支付成功消息后，将购买记录写入 purchase_records 表
func StartMQConsumer() {
	nameServers := mqNameServers()
	log.Printf("RocketMQ 使用的 NameServer 地址: %v", nameServers)
	c, err := rocketmq.NewPushConsumer(
		consumer.WithGroupName("verification_group"),
		consumer.WithNameServer(nameServers),
	)
	if err != nil {
		log.Printf("创建 MQ 消费者失败: %v", err)
		return
	}

	// 订阅 "payment_events" 主题
	err = c.Subscribe("payment_events", consumer.MessageSelector{}, func(ctx context.Context,
		msgs ...*primitive.MessageExt) (consumer.ConsumeResult, error) {

		for _, msg := range msgs {
			tag := msg.GetTags()
			log.Printf("收到 payment_events 消息 tag=%s body=%s", tag, msg.Body)

			if tag != "" && tag != "payment_succeeded" {
				log.Printf("忽略非支付成功的 tag=%s", tag)
				continue
			}

			var event PaymentSuccessEvent
			if err := json.Unmarshal(msg.Body, &event); err != nil {
				log.Printf("解析消息体失败: %v", err)
				continue
			}
			if event.AgentDid == "" || event.SkillDid == "" {
				log.Printf("消息缺少必要字段: agent/skill 为空, tx_id=%s", event.TxId)
				continue
			}

			record := PurchaseRecord{
				AgentDid: event.AgentDid,
				SkillDid: event.SkillDid,
				TxId:     event.TxId,
			}

			// DB 是 main.go 中定义的全局数据库连接
			if err := DB.Create(&record).Error; err != nil {
				if errors.Is(err, gorm.ErrDuplicatedKey) {
					log.Printf("purchase_records 唯一索引冲突，视为已处理: agent=%s skill=%s tx_id=%s", event.AgentDid, event.SkillDid, event.TxId)
					continue
				}
				log.Printf("写入 purchase_records 失败: %v", err)
				return consumer.ConsumeRetryLater, err
			}

			log.Printf("购买记录已保存 agent=%s skill=%s tx_id=%s", event.AgentDid, event.SkillDid, event.TxId)
		}

		return consumer.ConsumeSuccess, nil
	})

	if err != nil {
		log.Printf("订阅 MQ 主题失败: %v", err)
		return
	}

	// 启动消费者
	err = c.Start()
	if err != nil {
		log.Printf("启动 MQ 消费者失败: %v", err)
		return
	}
	log.Println("RocketMQ 消费者启动成功，等待消息...")
}