package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"stablepay/api-gateway/internal/application"
)

type RealPaymentClient struct {
	baseURL string
	hc      *http.Client
}

func NewRealPaymentClient(addr string) application.PaymentServiceClient {
	return &RealPaymentClient{
		baseURL: "http://" + addr,
		hc:      &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *RealPaymentClient) Pay(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/pay", bytes.NewReader(body))
	if err != nil {
		return nil, 500, 0, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if idempotencyKey, ok := req["idempotency_key"].(string); ok && idempotencyKey != "" {
		httpReq.Header.Set("X-Idempotency-Key", idempotencyKey)
	}
	res, err := c.hc.Do(httpReq)
	if err != nil {
		return nil, 500, 0, err
	}
	defer res.Body.Close()
	return c.parseResp(res)
}

func (c *RealPaymentClient) GetPaymentRequirement(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	values := url.Values{}
	for _, key := range []string{"skill_did", "agent_did", "skill_name", "price", "currency", "message"} {
		if value, ok := req[key]; ok && value != nil {
			str := fmt.Sprintf("%v", value)
			if str != "" {
				values.Set(key, str)
			}
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/pay/require?"+values.Encode(), nil)
	if err != nil {
		return nil, 500, 0, err
	}
	res, err := c.hc.Do(httpReq)
	if err != nil {
		return nil, 500, 0, err
	}
	defer res.Body.Close()
	return c.parseResp(res)
}

func (c *RealPaymentClient) GetPayment(ctx context.Context, txID string) (map[string]interface{}, int, int, error) {
	res, err := c.hc.Get(fmt.Sprintf("%s/api/v1/pay/%s", c.baseURL, txID))
	if err != nil {
		return nil, 500, 0, err
	}
	defer res.Body.Close()
	return c.parseResp(res)
}

func (c *RealPaymentClient) GetPaymentHistory(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	agentDID, _ := req["agent_did"].(string)
	url := fmt.Sprintf("%s/api/v1/pay/history?agent_did=%s", c.baseURL, agentDID)
	res, err := c.hc.Get(url)
	if err != nil {
		return nil, 500, 0, err
	}
	defer res.Body.Close()
	return c.parseResp(res)
}

func (c *RealPaymentClient) parseResp(res *http.Response) (map[string]interface{}, int, int, error) {
	data, _ := io.ReadAll(res.Body)
	var m map[string]interface{}
	_ = json.Unmarshal(data, &m)
	appCode := 0
	if code, ok := m["code"].(float64); ok {
		appCode = int(code)
	}
	if inner, ok := m["data"].(map[string]interface{}); ok {
		return inner, res.StatusCode, appCode, nil
	}
	return m, res.StatusCode, appCode, nil
}
