package dts

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// UnmarshalDTSEvent 使用 UseNumber 解析 DTSEvent，避免数字精度丢失
func UnmarshalDTSEvent(data []byte, event *DTSEvent) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return dec.Decode(event)
}

// PreprocessNumericFields 预处理数值字段，将DTS传输的字符串类型数值转换为int64
// DTS在传输tinyint/int/bigint类型时可能序列化为字符串，需要转换回数字以匹配实体字段类型
func PreprocessNumericFields(data map[string]any, fieldNames []string) map[string]any {
	result := make(map[string]any, len(data))
	for k, v := range data {
		result[k] = v
	}

	for _, fieldName := range fieldNames {
		if val, exists := result[fieldName]; exists && val != nil {
			if strVal, ok := val.(string); ok {
				var numVal int64
				if _, err := fmt.Sscanf(strVal, "%d", &numVal); err == nil {
					result[fieldName] = numVal
				}
			}
		}
	}

	return result
}

// GetStringValue 从map中获取字符串值
func GetStringValue(data map[string]any, key string) (string, bool) {
	val, ok := data[key]
	if !ok || val == nil {
		return "", false
	}
	str, ok := val.(string)
	return str, ok
}

// GetInt64Value 从map中获取int64值（支持字符串和数字类型）
func GetInt64Value(data map[string]any, key string) (int64, bool) {
	val, ok := data[key]
	if !ok || val == nil {
		return 0, false
	}

	switch v := val.(type) {
	case int64:
		return v, true
	case int:
		return int64(v), true
	case float64:
		return int64(v), true
	case string:
		var num int64
		if _, err := fmt.Sscanf(v, "%d", &num); err == nil {
			return num, true
		}
		return 0, false
	default:
		return 0, false
	}
}

// GetFloat64Value 从map中获取float64值（支持字符串和数字类型）
func GetFloat64Value(data map[string]any, key string) (float64, bool) {
	val, ok := data[key]
	if !ok || val == nil {
		return 0, false
	}

	switch v := val.(type) {
	case float64:
		return v, true
	case int64:
		return float64(v), true
	case int:
		return float64(v), true
	case string:
		var num float64
		if _, err := fmt.Sscanf(v, "%f", &num); err == nil {
			return num, true
		}
		return 0, false
	default:
		return 0, false
	}
}
