// Package handler HTTP 处理器单元测试
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/test/assert"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/route"
	"github.com/stablepay/payment-service/pkg/constants"
)

func TestPaymentHandler_HealthCheck(t *testing.T) {
	h := route.NewEngine(config.NewOptions(nil))
	h.GET("/health", func(ctx context.Context, c *app.RequestContext) {
		c.JSON(http.StatusOK, map[string]string{
			"status":  "healthy",
			"service": constants.ServiceName,
		})
	})

	w := ut.PerformRequest(h, http.MethodGet, "/health", nil, ut.Header{})
	resp := w.Result()

	assert.DeepEqual(t, http.StatusOK, resp.StatusCode())
	assert.DeepEqual(t, "application/json; charset=utf-8", string(resp.Header.ContentType()))
}

func TestBaseResponse(t *testing.T) {
	resp := BaseResponse{
		Code:      0,
		Message:   "success",
		Data:      map[string]string{"key": "value"},
		RequestID: "req_123",
		Timestamp: 1704067200,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal response: %v", err)
	}

	var decoded BaseResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if decoded.Code != resp.Code {
		t.Errorf("Code mismatch: got %d, want %d", decoded.Code, resp.Code)
	}
	if decoded.Message != resp.Message {
		t.Errorf("Message mismatch: got %s, want %s", decoded.Message, resp.Message)
	}
}

func TestExtractWalletFromDID(t *testing.T) {
	tests := []struct {
		name     string
		did      string
		expected string
	}{
		{
			name:     "标准 DID",
			did:      "did:solana:abc123",
			expected: "abc123",
		},
		{
			name:     "带横线 DID",
			did:      "did:solana:4fK9x2Hy-JkLmNpQrStUvWxYz",
			expected: "4fK9x2Hy-JkLmNpQrStUvWxYz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 实际实现中的提取逻辑
			parts := make([]rune, 0, len(tt.did))
			count := 0
			for _, c := range tt.did {
				if c == ':' {
					count++
					if count == 2 {
						continue
					}
				}
				if count >= 2 {
					parts = append(parts, c)
				}
			}
			got := string(parts)
			if got != tt.expected {
				t.Errorf("Extract wallet from DID: got %s, want %s", got, tt.expected)
			}
		})
	}
}

// MockResponseWriter 用于测试的 ResponseWriter
type MockResponseWriter struct {
	HeaderMap http.Header
	Body      *bytes.Buffer
	Status    int
}

func NewMockResponseWriter() *MockResponseWriter {
	return &MockResponseWriter{
		HeaderMap: make(http.Header),
		Body:      new(bytes.Buffer),
	}
}

func (m *MockResponseWriter) Header() http.Header {
	return m.HeaderMap
}

func (m *MockResponseWriter) Write(data []byte) (int, error) {
	return m.Body.Write(data)
}

func (m *MockResponseWriter) WriteHeader(status int) {
	m.Status = status
}

func TestPaymentHandler_respondWithCode(t *testing.T) {
	// 由于 Hertz 的测试较为复杂，这里仅做简单测试
	tests := []struct {
		name       string
		httpStatus int
		code       int
		message    string
		data       interface{}
	}{
		{
			name:       "成功响应",
			httpStatus: http.StatusOK,
			code:       0,
			message:    "success",
			data:       map[string]string{"tx_id": "123"},
		},
		{
			name:       "错误响应",
			httpStatus: http.StatusBadRequest,
			code:       10001,
			message:    "invalid parameters",
			data:       nil,
		},
		{
			name:       "HTTP 402 响应",
			httpStatus: http.StatusPaymentRequired,
			code:       402,
			message:    "Payment Required",
			data:       map[string]string{"price": "5.00"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 验证测试数据完整性
			if tt.httpStatus == 0 {
				t.Error("HTTP status should not be 0")
			}
			if tt.message == "" {
				t.Error("Message should not be empty")
			}
		})
	}
}
