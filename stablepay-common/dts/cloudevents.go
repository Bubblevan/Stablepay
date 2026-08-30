package dts

import (
	"encoding/json"
)

// CloudEventsMessage CloudEvents 格式的消息（DTS使用此格式）
type CloudEventsMessage struct {
	Data            json.RawMessage `json:"data"`             // 实际的DTS事件数据
	ID              string          `json:"id"`               // 消息ID（用作幂等键）
	Source          string          `json:"source"`           // 消息来源
	SpecVersion     string          `json:"specversion"`      // CloudEvents规范版本
	Type            string          `json:"type"`             // 消息类型
	DataContentType string          `json:"datacontenttype"`  // 数据内容类型
	Time            string          `json:"time"`             // 消息时间
	Subject         string          `json:"subject"`          // 主题
	AliyunAccountID string          `json:"aliyunaccountid"`  // 阿里云账号ID
}
