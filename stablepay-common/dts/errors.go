package dts

import (
	"encoding/json"
	"errors"
	"fmt"
)

// DataMappingError 数据映射错误（结构不匹配、字段缺失等）
// 此类错误表明消息格式有问题，应该跳过并继续消费
type DataMappingError struct {
	Table   string         `json:"table"`
	Field   string         `json:"field"`
	Message string         `json:"message"`
	RawData map[string]any `json:"raw_data"` // 原始数据，用于诊断
	Cause   error          `json:"-"`
}

func (e *DataMappingError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("data mapping error for table %s (field: %s): %s, cause: %v",
			e.Table, e.Field, e.Message, e.Cause)
	}
	return fmt.Sprintf("data mapping error for table %s (field: %s): %s",
		e.Table, e.Field, e.Message)
}

func (e *DataMappingError) Unwrap() error {
	return e.Cause
}

// GetRawDataJSON 返回原始数据的JSON字符串
func (e *DataMappingError) GetRawDataJSON() string {
	if e.RawData == nil {
		return "{}"
	}
	data, err := json.Marshal(e.RawData)
	if err != nil {
		return fmt.Sprintf("{\"error\": \"failed to marshal raw data: %v\"}", err)
	}
	return string(data)
}

// IsDataMappingError 标记此错误为数据映射错误
func (e *DataMappingError) IsDataMappingError() bool {
	return true
}

// NewDataMappingError 创建数据映射错误
func NewDataMappingError(table, field, message string, rawData map[string]any, cause error) error {
	return &DataMappingError{
		Table:   table,
		Field:   field,
		Message: message,
		RawData: rawData,
		Cause:   cause,
	}
}

// IsDataMappingError 检查错误是否为数据映射错误
// 数据映射错误表示消息格式有问题，应该跳过而不是重试
func IsDataMappingError(err error) bool {
	if err == nil {
		return false
	}

	// 尝试断言为 DataMappingError 接口
	var mappingErr interface {
		IsDataMappingError() bool
	}
	return errors.As(err, &mappingErr)
}
