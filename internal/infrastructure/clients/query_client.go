package clients

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"stablepay/api-gateway/internal/application"
)

// RealQueryClient calls query-service through its HTTP adapter.
type RealQueryClient struct {
	baseURL string
	hc      *http.Client
}

func NewRealQueryClient(addr string) application.QueryServiceClient {
	return &RealQueryClient{
		baseURL: "http://" + addr,
		hc:      &http.Client{Timeout: 3 * time.Second},
	}
}

func (c *RealQueryClient) GetBalance(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	agentDID, _ := req["agent_did"].(string)
	res, err := c.hc.Get(fmt.Sprintf("%s/internal/balance?agent_did=%s", c.baseURL, agentDID))
	if err != nil {
		return nil, 500, 0, err
	}
	defer res.Body.Close()
	return c.decode(res)
}

func (c *RealQueryClient) GetTransactions(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	did, _ := req["did"].(string)
	txType, _ := req["type"].(string)
	if txType == "" {
		txType = "1"
	}
	url := fmt.Sprintf("%s/internal/transactions?did=%s&type=%s", c.baseURL, did, txType)
	res, err := c.hc.Get(url)
	if err != nil {
		return nil, 500, 0, err
	}
	defer res.Body.Close()
	return c.decode(res)
}

func (c *RealQueryClient) GetRevenue(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	skillDID, _ := req["skill_did"].(string)
	res, err := c.hc.Get(fmt.Sprintf("%s/internal/revenue?skill_did=%s", c.baseURL, skillDID))
	if err != nil {
		return nil, 500, 0, err
	}
	defer res.Body.Close()
	return c.decode(res)
}

func (c *RealQueryClient) GetSales(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	skillDID, _ := req["skill_did"].(string)
	res, err := c.hc.Get(fmt.Sprintf("%s/internal/sales?skill_did=%s", c.baseURL, skillDID))
	if err != nil {
		return nil, 500, 0, err
	}
	defer res.Body.Close()
	return c.decode(res)
}

func (c *RealQueryClient) decode(res *http.Response) (map[string]interface{}, int, int, error) {
	data, _ := io.ReadAll(res.Body)
	var m map[string]interface{}
	_ = json.Unmarshal(data, &m)
	return m, res.StatusCode, extractAppCode(m), nil
}
