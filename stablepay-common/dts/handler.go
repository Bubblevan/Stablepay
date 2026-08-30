package dts

import (
	"context"
	"encoding/json"

	"github.com/apache/rocketmq-clients/golang/v5"
)

// Handler DTS事件处理器接口
// 业务代码需要实现此接口来处理具体的表同步事件
type Handler interface {
	// Handle 处理DTS事件
	// ctx: 上下文
	// event: DTS事件适配器，包含完整的变更数据
	// 返回值: 如果返回错误，消息处理器会根据错误类型决定是否重试
	Handle(ctx context.Context, event *DTSEventAdapter) error
}

// TableRouter 表路由函数类型
type TableRouter func(tableName string) (Handler, bool)

// Logger 日志接口
// 由调用者实现并传递给 MessageHandler
// 与 code.wenfu.cn/stablepay/stablepay-common/log 包的 Logger 接口保持一致
type Logger interface {
	Info(ctx context.Context, msg string, fields map[string]interface{})
	Error(ctx context.Context, msg string, fields map[string]interface{})
	Warn(ctx context.Context, msg string, fields map[string]interface{})
	Debug(ctx context.Context, msg string, fields map[string]interface{})
}

// MessageHandler DTS消息处理器
// 负责接收RocketMQ消息、解析CloudEvents格式、并路由到具体的处理器
type MessageHandler struct {
	router TableRouter
	logger Logger
}

// NewMessageHandler 创建DTS消息处理器
// router: 表路由函数，根据表名返回对应的处理器
// logger: 日志记录器，由调用者提供；如果为 nil，则不记录日志
func NewMessageHandler(router TableRouter, logger Logger) *MessageHandler {
	return &MessageHandler{
		router: router,
		logger: logger,
	}
}

// HandleMessage 处理RocketMQ消息的统一入口
// 负责：
// 1. 解析外层 CloudEvents 消息
// 2. 解析内层 DTSEvent
// 3. 适配为 DTSEventAdapter
// 4. 路由到具体的业务处理器
func (h *MessageHandler) HandleMessage(ctx context.Context, messageView *golang.MessageView) error {
	messageID := messageView.GetMessageId()
	topic := messageView.GetTopic()

	// 获取消息体
	body := messageView.GetBody()

	// 1. 解析外层 CloudEvents 消息
	var cloudEvent CloudEventsMessage
	if err := json.Unmarshal(body, &cloudEvent); err != nil {
		if h.logger != nil {
			h.logger.Warn(ctx, "Failed to unmarshal CloudEvents message", map[string]interface{}{
				"message_id":  messageID,
				"raw_body":    string(body),
				"body_length": len(body),
				"error":       err.Error(),
			})
		}
		// CloudEvents消息格式错误，直接返回成功避免重复消费
		return nil
	}

	// 2. 解析 DTSEvent
	var raw DTSEvent
	if err := UnmarshalDTSEvent(cloudEvent.Data, &raw); err != nil {
		if h.logger != nil {
			h.logger.Warn(ctx, "Failed to unmarshal DTSEvent from CloudEvents data", map[string]interface{}{
				"message_id":     messageID,
				"cloudevents_id": cloudEvent.ID,
				"raw_data":       string(cloudEvent.Data),
				"error":          err.Error(),
			})
		}
		// DTS Event格式错误，直接返回成功避免重复消费
		return nil
	}

	// 3. 适配为 DTSEventAdapter，使用 CloudEvents 的 ID 作为幂等键
	dtsEvt, err := NewDTSEventAdapter(cloudEvent.ID, &raw, topic)
	if err != nil {
		if h.logger != nil {
			// 序列化原始 DTSEvent 用于日志
			rawDataBytes, _ := json.Marshal(raw)
			h.logger.Warn(ctx, "Failed to adapt DTSEvent", map[string]interface{}{
				"message_id":     messageID,
				"cloudevents_id": cloudEvent.ID,
				"raw_dts_event":  string(rawDataBytes),
				"error":          err.Error(),
			})
		}
		// 适配失败，直接返回成功避免重复消费
		return nil
	}

	// 4. 根据表名路由到不同的处理器
	if err := h.routeToHandler(ctx, dtsEvt); err != nil {
		// 检查是否为数据映射错误
		if IsDataMappingError(err) {
			// 数据映射错误表明消息格式有问题，无法解析
			// 记录警告日志，但返回nil让消息正常消费完成，避免重复消费
			if h.logger != nil {
				// 序列化完整的事件数据用于排查
				eventDataBytes, _ := json.Marshal(dtsEvt.GetEventData())
				h.logger.Warn(ctx, "Skipping message due to data mapping error", map[string]interface{}{
					"message_id":     messageID,
					"dts_event_id":   dtsEvt.GetID(),
					"table":          dtsEvt.GetTableName(),
					"operation_type": dtsEvt.GetType(),
					"event_data":     string(eventDataBytes),
					"error":          err.Error(),
				})
			}
			return nil
		}

		// 其他错误（如数据库错误），返回错误触发重试
		if h.logger != nil {
			// 序列化完整的事件数据用于排查
			eventDataBytes, _ := json.Marshal(dtsEvt.GetEventData())
			h.logger.Error(ctx, "Failed to handle DTS event", map[string]interface{}{
				"message_id":   messageID,
				"dts_event_id": dtsEvt.GetID(),
				"table":        dtsEvt.GetTableName(),
				"event_data":   string(eventDataBytes),
				"error":        err.Error(),
			})
		}
		return err
	}

	return nil
}

// routeToHandler 根据表名路由到不同的处理器
func (h *MessageHandler) routeToHandler(ctx context.Context, dtsEvt *DTSEventAdapter) error {
	tableName := dtsEvt.GetTableName()

	// 忽略DDL事件（可以根据需要处理）
	if dtsEvt.GetType() == "DDL" {
		return nil
	}

	// 根据表名路由
	handler, found := h.router(tableName)
	if !found {
		// 未知表，记录警告但不返回错误
		if h.logger != nil {
			h.logger.Warn(ctx, "Unknown table in DTS event", map[string]interface{}{
				"event_id":       dtsEvt.GetID(),
				"table":          tableName,
				"operation_type": dtsEvt.GetType(),
			})
		}
		return nil
	}

	return handler.Handle(ctx, dtsEvt)
}
