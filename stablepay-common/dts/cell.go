package dts

import (
	"encoding/json"
	"time"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/encoding/unicode"
)

// ToValue 将Cell中的原始数据转换为Go的原生类型
func (c Cell) ToValue() (any, error) {
	// ③ epoch型时间：{"timestampSec":..., "micro":...}
	if c.TimestampSec != nil {
		sec := *c.TimestampSec
		micro := int64(0)
		if c.Micro != nil {
			micro = *c.Micro
		}
		// micro 秒 -> 纳秒
		t := time.Unix(sec, micro*1_000).UTC()
		return t, nil
	}

	// ② 分解型时间：Year/Month/...
	if c.Year != nil && c.Month != nil {
		ns := 0
		if c.Naons != nil {
			ns = *c.Naons
		}
		t := time.Date(*c.Year, time.Month(*c.Month), derefOr(c.Day, 1),
			derefOr(c.Hour, 0), derefOr(c.Minute, 0), derefOr(c.Second, 0), ns, time.UTC)
		return t, nil
	}

	// 空值
	if len(c.Data) == 0 || string(c.Data) == "null" {
		return nil, nil
	}

	// 文本（hb 编码）
	var h hbText
	if json.Unmarshal(c.Data, &h) == nil && len(h.Hb) > 0 {
		// 将有符号 int 数组转换为无符号 byte 数组
		// Java 的 byte 范围是 -128 到 127，需要转换为 Go 的 0 到 255
		bytes := make([]byte, len(h.Hb))
		for i, v := range h.Hb {
			bytes[i] = byte(v)
		}
		// 根据 charset 进行字符编码转换
		return decodeBytes(bytes, c.Charset)
	}

	// 检测 ByteBuffer 对象（BLOB/BINARY 字段）
	// ByteBuffer 对象包含 offset、position、limit、capacity 等字段
	var bufferObj map[string]any
	if json.Unmarshal(c.Data, &bufferObj) == nil {
		// 检测是否为 ByteBuffer：存在 offset、position、limit、capacity 字段
		if _, hasOffset := bufferObj["offset"]; hasOffset {
			if _, hasPosition := bufferObj["position"]; hasPosition {
				if _, hasLimit := bufferObj["limit"]; hasLimit {
					if _, hasCapacity := bufferObj["capacity"]; hasCapacity {
						// 这是一个 ByteBuffer 对象
						// 尝试提取 hb 字节数组
						if hbVal, hasHb := bufferObj["hb"]; hasHb {
							// hb 在 JSON 中是数字数组，会被解析为 []interface{}
							if hbArray, ok := hbVal.([]interface{}); ok && len(hbArray) > 0 {
								// 将 interface{} 数组转换为 byte 数组
								bytes := make([]byte, len(hbArray))
								for i, v := range hbArray {
									// JSON 数字默认解析为 float64
									if num, ok := v.(float64); ok {
										bytes[i] = byte(int(num))
									}
								}
								// 根据 charset 进行字符编码转换
								return decodeBytes(bytes, c.Charset)
							}
							// hb 可能是空数组或其他格式，返回 nil
						}
						// ByteBuffer 但没有有效数据，返回 nil
						return nil, nil
					}
				}
			}
		}
	}

	// 数字/十进制：返回字符串避免精度丢失
	var num json.Number
	if json.Unmarshal(c.Data, &num) == nil {
		return num.String(), nil
	}

	// 元数据这类：{"objectType":"JSON","data":"{\"k\":\"v\"}"}
	var obj struct {
		ObjectType string          `json:"objectType"`
		Data       json.RawMessage `json:"data"`
	}
	if json.Unmarshal(c.Data, &obj) == nil && obj.ObjectType == "JSON" && len(obj.Data) > 0 {
		// 尝试把 data 里的 JSON 字符串再反序列化成任意类型
		var anyv any
		if json.Unmarshal(obj.Data, &anyv) == nil {
			return anyv, nil
		}
		return string(obj.Data), nil
	}

	// 兜底：原始 JSON 字符串
	return string(c.Data), nil
}

// decodeBytes 根据字符集将字节数组转换为字符串
func decodeBytes(data []byte, charset string) (any, error) {
	if len(data) == 0 {
		return "", nil
	}

	// 如果没有指定字符集或者是 UTF-8，直接转换
	// utf8mb4 是 MySQL 的字符集，实际上就是标准的 UTF-8（支持 4 字节字符）
	if charset == "" ||
		charset == "utf8" || charset == "UTF-8" || charset == "utf-8" ||
		charset == "utf8mb4" || charset == "UTF8MB4" {
		return string(data), nil
	}

	// 特殊处理 latin1（ISO-8859-1）
	// latin1 是单字节编码，每个字节直接对应 Unicode 码点 U+0000 到 U+00FF
	if charset == "latin1" || charset == "ISO-8859-1" || charset == "iso-8859-1" {
		runes := make([]rune, len(data))
		for i, b := range data {
			runes[i] = rune(b)
		}
		return string(runes), nil
	}

	// 获取字符集解码器
	decoder := getCharsetDecoder(charset)
	if decoder == nil {
		// 不支持的字符集，直接返回原始字符串（降级处理）
		return string(data), nil
	}

	// 解码
	decoded, err := decoder.Bytes(data)
	if err != nil {
		// 解码失败，返回原始字符串
		return string(data), nil
	}

	return string(decoded), nil
}

// getCharsetDecoder 根据字符集名称获取解码器
func getCharsetDecoder(charset string) *encoding.Decoder {
	switch charset {
	case "latin1", "ISO-8859-1", "iso-8859-1":
		// latin1 是单字节编码，每个字节直接对应一个 Unicode 码点（U+0000 到 U+00FF）
		// 但 golang.org/x/text/encoding 没有提供 latin1 解码器
		// 对于 latin1，我们可以手动转换
		return nil
	case "gbk", "GBK", "gb2312", "GB2312":
		// 简体中文
		return simplifiedchinese.GBK.NewDecoder()
	case "big5", "BIG5", "Big5":
		// 繁体中文
		return traditionalchinese.Big5.NewDecoder()
	case "utf16", "UTF-16", "utf-16":
		return unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM).NewDecoder()
	case "utf16be", "UTF-16BE", "utf-16be":
		return unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM).NewDecoder()
	case "utf16le", "UTF-16LE", "utf-16le":
		return unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM).NewDecoder()
	default:
		return nil
	}
}

func derefOr(p *int, def int) int {
	if p == nil {
		return def
	}
	return *p
}
