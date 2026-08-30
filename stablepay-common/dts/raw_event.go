package dts

import (
	"encoding/json"
)

// DTSEvent 是从DTS接收到的原始事件结构（仅包含所需字段）
type DTSEvent struct {
	ID              int64  `json:"id"`
	OperationType   string `json:"operationType"` // INSERT / UPDATE / DELETE / DDL
	Source          string `json:"source"`
	SourceTimestamp *int64 `json:"sourceTimestamp,omitempty"`

	Schema struct {
		DatabaseName string `json:"databaseName"`
		TableName    string `json:"tableName"`
		RecordFields []struct {
			FieldName      string `json:"fieldName"`
			RawDataTypeNum int    `json:"rawDataTypeNum"`
			FieldPosition  int    `json:"fieldPosition"`
		} `json:"recordFields"`
	} `json:"schema"`

	BeforeImage *Image `json:"beforeImage,omitempty"`
	AfterImage  *Image `json:"afterImage,omitempty"`
}

// Image 代表一个数据"镜像"中的一行数据
type Image struct {
	RecordSchema struct {
		RecordFields []struct {
			FieldName      string `json:"fieldName"`
			RawDataTypeNum int    `json:"rawDataTypeNum"`
			FieldPosition  int    `json:"fieldPosition"`
		} `json:"recordFields"`
	} `json:"recordSchema"`

	Values []Cell `json:"values"`
	Size   int    `json:"size"`
}

// Cell 是一个可以容纳多种数据类型的"单元格"联合类型
type Cell struct {
	// ① 文本或数字形态
	Data    json.RawMessage `json:"data,omitempty"`
	Charset string          `json:"charset,omitempty"`

	// ② 时间对象（分解型）
	Year   *int `json:"year,omitempty"`
	Month  *int `json:"month,omitempty"`
	Day    *int `json:"day,omitempty"`
	Hour   *int `json:"hour,omitempty"`
	Minute *int `json:"minute,omitempty"`
	Second *int `json:"second,omitempty"`
	Naons  *int `json:"naons,omitempty"` // 原样保留

	// ③ 时间对象（epoch型）
	TimestampSec *int64 `json:"timestampSec,omitempty"`
	Micro        *int64 `json:"micro,omitempty"`

	Segments *int `json:"segments,omitempty"` // 可忽略
}

// hbText 用于解析 {"hb":[...]} 格式的文本载体
// 注意：DTS 的 Java 实现使用有符号 byte[]，范围 -128 到 127
// 所以 JSON 中可能包含负数，需要使用 []int8 来解析
type hbText struct {
	Hb []int8 `json:"hb"`
}

// ImageToRow 将一个数据镜像（Image）根据schema转换为 map[string]any 格式的行数据
func (e *DTSEvent) ImageToRow(img *Image) (map[string]any, error) {
	row := make(map[string]any)
	if img == nil {
		return row, nil
	}
	fields := img.RecordSchema.RecordFields // ← 用 image 自带的 schema
	for i, f := range fields {
		if i >= len(img.Values) {
			row[f.FieldName] = nil
			continue
		}
		v, err := img.Values[i].ToValue()
		if err != nil {
			return nil, err
		}
		row[f.FieldName] = v
	}
	return row, nil
}
