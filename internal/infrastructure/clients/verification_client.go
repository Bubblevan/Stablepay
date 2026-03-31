package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"stablepay/api-gateway/internal/application"
)

// RealVerificationClient 调用 verification-service 的 HTTP 内部接口（:8185）
type RealVerificationClient struct {
	baseURL string
	hc      *http.Client
}

func NewRealVerificationClient(addr string) application.VerificationServiceClient {
	return &RealVerificationClient{
		baseURL: "http://" + addr,
		hc:      &http.Client{Timeout: 3 * time.Second},
	}
}

func (c *RealVerificationClient) Verify(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"agent_did": req["agent_did"],
		"skill_did": req["skill_did"],
	})
	res, err := c.hc.Post(c.baseURL+"/internal/verify", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, 500, 0, err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	var m map[string]interface{}
	_ = json.Unmarshal(data, &m)
	return m, res.StatusCode, extractAppCode(m), nil
}

func (c *RealVerificationClient) BatchVerify(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"agent_did":  req["agent_did"],
		"skill_dids": req["skill_dids"],
	})
	res, err := c.hc.Post(c.baseURL+"/internal/verify/batch", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, 500, 0, err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	var m map[string]interface{}
	_ = json.Unmarshal(data, &m)
	return m, res.StatusCode, extractAppCode(m), nil
}

func (c *RealVerificationClient) GetProof(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	agentDID, _ := req["agent_did"].(string)
	skillDID, _ := req["skill_did"].(string)
	url := c.baseURL + "/internal/verify/proof?agent_did=" + agentDID + "&skill_did=" + skillDID
	res, err := c.hc.Get(url)
	if err != nil {
		return nil, 500, 0, err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	var m map[string]interface{}
	_ = json.Unmarshal(data, &m)
	return m, res.StatusCode, extractAppCode(m), nil
}
