package dts

import (
	"strconv"
	"time"
)

// DTSEventData 是 DTS 事件适配后的统一数据结构
type DTSEventData struct {
	EventID   string         `json:"event_id"`   // 幂等键
	EventType string         `json:"event_type"` // 操作类型：INSERT/UPDATE/DELETE/DDL
	Schema    DTSSchemaInfo  `json:"schema"`     // 表信息
	Data      DTSPayloadData `json:"data"`       // 数据负载
}

// DTSSchemaInfo DTS schema 信息
type DTSSchemaInfo struct {
	DatabaseName string `json:"database_name"`
	TableName    string `json:"table_name"`
}

// DTSPayloadData DTS 数据负载
type DTSPayloadData struct {
	Payload DTSPayload `json:"payload"`
}

// DTSPayload DTS 负载详情
type DTSPayload struct {
	Before   map[string]any `json:"before,omitempty"`   // 变更前数据（DML）
	After    map[string]any `json:"after,omitempty"`    // 变更后数据（DML）
	DDL      string         `json:"ddl,omitempty"`      // DDL语句（DDL）
	Position string         `json:"position,omitempty"` // DTS位点
}

// DTSEventAdapter 将底层的 DTSEvent 适配成统一的事件结构
type DTSEventAdapter struct {
	id              string // 幂等键，使用 dtsMessage.ID
	raw             *DTSEvent
	payload         *DTSEventData
	topic           string // 原始 topic 信息
	sourceTimestamp time.Time
}

// NewDTSEventAdapter 创建一个新的DTSEvent适配器实例
// id: dtsMessage.ID，作为幂等键
// raw: DTSEvent，包含数据变更详情
// topic: 原始 RocketMQ topic
func NewDTSEventAdapter(id string, raw *DTSEvent, topic string) (*DTSEventAdapter, error) {
	a := &DTSEventAdapter{
		id:    id,
		raw:   raw,
		topic: topic,
	}

	// 解析变更前后的数据
	after, err := raw.ImageToRow(raw.AfterImage)
	if err != nil {
		return nil, err
	}
	before, err := raw.ImageToRow(raw.BeforeImage)
	if err != nil {
		return nil, err
	}

	// 构建包含 event_id、event_type 和 position 的完整数据
	eventData := &DTSEventData{
		EventID:   id, // 使用 dtsMessage.ID 作为幂等键
		EventType: raw.OperationType,
		Schema: DTSSchemaInfo{
			DatabaseName: raw.Schema.DatabaseName,
			TableName:    raw.Schema.TableName,
		},
		Data: DTSPayloadData{
			Payload: DTSPayload{
				Position: strconv.FormatInt(raw.ID, 10), // raw.ID 作为 DTS 位点
			},
		},
	}

	if raw.OperationType == "DDL" {
		// DDL事件：payload包含ddl文本和position
		if ddl, ok := after["ddl"]; ok {
			if ddlStr, ok := ddl.(string); ok {
				eventData.Data.Payload.DDL = ddlStr
			}
		}
	} else {
		// DML事件：payload包含before、after和position
		eventData.Data.Payload.Before = before
		eventData.Data.Payload.After = after
	}

	// 解析源时间戳
	if raw.SourceTimestamp != nil && *raw.SourceTimestamp > 0 {
		a.sourceTimestamp = time.Unix(*raw.SourceTimestamp, 0).UTC()
	} else {
		a.sourceTimestamp = time.Now().UTC()
	}

	a.payload = eventData
	return a, nil
}

// GetID 返回幂等键 (dtsMessage.ID)
func (e *DTSEventAdapter) GetID() string {
	return e.id
}

// GetType 返回操作类型（INSERT/UPDATE/DELETE/DDL）
func (e *DTSEventAdapter) GetType() string {
	return e.raw.OperationType
}

// GetOccurredOn 返回事件发生时间
func (e *DTSEventAdapter) GetOccurredOn() time.Time {
	return e.sourceTimestamp
}

// GetData 返回适配后的完整事件数据（实现 Event 接口）
func (e *DTSEventAdapter) GetData() any {
	return e.payload
}

// GetEventData 返回具体的 DTSEventData 类型（类型安全的访问方法）
func (e *DTSEventAdapter) GetEventData() *DTSEventData {
	return e.payload
}

// GetSource 返回数据源
func (e *DTSEventAdapter) GetSource() string {
	return e.raw.Source
}

// GetTopic 返回原始 topic
func (e *DTSEventAdapter) GetTopic() string {
	return e.topic
}

// GetDatabaseName 返回数据库名
func (e *DTSEventAdapter) GetDatabaseName() string {
	return e.raw.Schema.DatabaseName
}

// GetTableName 返回表名
func (e *DTSEventAdapter) GetTableName() string {
	return e.raw.Schema.TableName
}

// GetBeforeImage 返回变更前的数据
func (e *DTSEventAdapter) GetBeforeImage() map[string]any {
	if e.payload != nil {
		return e.payload.Data.Payload.Before
	}
	return nil
}

// GetAfterImage 返回变更后的数据
func (e *DTSEventAdapter) GetAfterImage() map[string]any {
	if e.payload != nil {
		return e.payload.Data.Payload.After
	}
	return nil
}

// GetPosition 返回DTS位点
func (e *DTSEventAdapter) GetPosition() string {
	if e.payload != nil {
		return e.payload.Data.Payload.Position
	}
	return ""
}

// Verify DTSEventAdapter implements Event interface
var _ Event = (*DTSEventAdapter)(nil)
