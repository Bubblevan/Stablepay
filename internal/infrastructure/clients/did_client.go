package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"stablepay/api-gateway/internal/application"
)

// RealDIDClient calls did-service through its HTTP adapter.
type RealDIDClient struct {
	baseURL string
	hc      *http.Client
}

func NewRealDIDClient(addr string) application.DIDServiceClient {
	return &RealDIDClient{
		baseURL: "http://" + addr,
		hc:      &http.Client{Timeout: 3 * time.Second},
	}
}

func (c *RealDIDClient) CreateDID(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"user_type": req["user_type"],
		"metadata":  req["metadata"],
	})
	resp, err := c.post(ctx, "/internal/did", body)
	if err != nil {
		return nil, 500, 0, err
	}
	return flattenDIDCreate(resp), 200, extractAppCode(resp), nil
}

func (c *RealDIDClient) RegisterDID(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"user_type":      req["user_type"],
		"public_key":     req["public_key"],
		"wallet_address": req["wallet_address"],
		"wallet_id":      req["wallet_id"],
		"metadata":       req["metadata"],
	})
	resp, err := c.post(ctx, "/internal/did/register", body)
	if err != nil {
		return nil, 500, 0, err
	}
	return flattenDIDRegister(resp), 200, extractAppCode(resp), nil
}

func (c *RealDIDClient) VerifyDID(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"did":       req["did"],
		"message":   req["message"],
		"signature": req["signature"],
		"timestamp": req["timestamp"],
		"nonce":     req["nonce"],
	})
	resp, err := c.post(ctx, "/internal/did/verify-sig", body)
	if err != nil {
		return nil, 500, 0, err
	}
	return map[string]interface{}{"valid": resp["valid"]}, 200, extractAppCode(resp), nil
}

func (c *RealDIDClient) GetDID(ctx context.Context, did string) (map[string]interface{}, int, int, error) {
	raw, status, err := c.doRequest(ctx, http.MethodGet, "/internal/did/"+did, nil)
	if err != nil {
		return nil, 500, 0, err
	}
	if status != 200 {
		return nil, status, 0, fmt.Errorf("did-service returned %d", status)
	}
	return map[string]interface{}{
		"did":            raw["did"],
		"public_key":     raw["public_key"],
		"wallet_address": raw["wallet_address"],
	}, 200, extractAppCode(raw), nil
}

func (c *RealDIDClient) VerifySignature(ctx context.Context, req map[string]interface{}) (bool, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"did":       req["did"],
		"message":   req["message"],
		"signature": req["signature"],
		"timestamp": req["timestamp"],
		"nonce":     req["nonce"],
	})
	resp, err := c.post(ctx, "/internal/did/verify-sig", body)
	if err != nil {
		return false, err
	}
	valid, _ := resp["valid"].(bool)
	return valid, nil
}

func (c *RealDIDClient) post(ctx context.Context, path string, body []byte) (map[string]interface{}, error) {
	m, _, err := c.doRequest(ctx, http.MethodPost, path, body)
	return m, err
}

func (c *RealDIDClient) doRequest(ctx context.Context, method, path string, body []byte) (map[string]interface{}, int, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.hc.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	var m map[string]interface{}
	_ = json.Unmarshal(data, &m)
	return m, res.StatusCode, nil
}

func extractAppCode(m map[string]interface{}) int {
	if base, ok := m["base"].(map[string]interface{}); ok {
		if code, ok := base["code"].(float64); ok {
			return int(code)
		}
	}
	return 0
}

func flattenDIDCreate(m map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"did":            m["did"],
		"public_key":     m["public_key"],
		"wallet_address": m["wallet_address"],
		"created_at":     m["created_at"],
	}
}

func flattenDIDRegister(m map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"did":            firstNonNil(m["did"], m["did_string"], m["DIDString"]),
		"public_key":     firstNonNil(m["public_key"], m["PublicKey"]),
		"wallet_address": firstNonNil(m["wallet_address"], m["WalletAddress"]),
		"wallet_id":      firstNonNil(m["wallet_id"], m["WalletID"]),
		"status":         firstNonNil(m["status"], m["Status"]),
		"created_at":     firstNonNil(m["created_at"], m["CreatedAt"]),
	}
}

func firstNonNil(values ...interface{}) interface{} {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

