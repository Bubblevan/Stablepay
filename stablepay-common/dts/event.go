package dts

import (
	"time"
)

// Event 消息队列事件接口（统一的事件抽象）
type Event interface {
	GetID() string            // 获取事件ID（幂等键）
	GetType() string          // 获取事件类型
	GetOccurredOn() time.Time // 获取事件发生时间
	GetData() any             // 获取事件数据
	GetSource() string        // 获取事件来源
	GetTopic() string         // 获取原始 topic 信息
}
