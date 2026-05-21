package clients

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/kitex/client"
	"github.com/cloudwego/kitex/pkg/retry"
	"stablepay/api-gateway/internal/application"
	"stablepay/api-gateway/kitex_gen/stablepay/common"
	"stablepay/api-gateway/kitex_gen/stablepay/did_service"
	"stablepay/api-gateway/kitex_gen/stablepay/did_service/didservice"
)

// KitexDIDClient 通过 Kitex RPC 调用 did-service（与 did-service 监听端口一致，默认 8081）。
type KitexDIDClient struct {
	cli didservice.Client
}

// NewKitexDIDClient 创建客户端。destService 为 Kitex 目标服务名；hostPort 为 TCP 地址，例如 stablepay-did-service:8081。
func NewKitexDIDClient(destService, hostPort string, timeoutMs, retryCount int) (application.DIDServiceClient, error) {
	opts := []client.Option{
		client.WithHostPorts(hostPort),
		client.WithResolver(nil),
	}
	if timeoutMs > 0 {
		opts = append(opts, client.WithRPCTimeout(time.Duration(timeoutMs)*time.Millisecond))
	}
	if retryCount > 0 {
		opts = append(opts, client.WithFailureRetry(retry.NewFailurePolicy()))
	}
	cli, err := didservice.NewClient(destService, opts...)
	if err != nil {
		return nil, fmt.Errorf("kitex did client: %w", err)
	}
	return &KitexDIDClient{cli: cli}, nil
}

func (c *KitexDIDClient) CreateDID(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	kreq := did_service.NewCreateDIDRequest()
	kreq.UserType = parseUserType(req["user_type"])
	kreq.Metadata = mergeMetadata(nil, req["metadata"])
	resp, err := c.cli.CreateDID(ctx, kreq)
	if err != nil {
		return nil, 500, 0, err
	}
	return flattenDIDCreateRPC(resp), 200, int(resp.GetBase().GetCode()), nil
}

func (c *KitexDIDClient) RegisterDID(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	kreq := did_service.NewRegisterDIDRequest()
	kreq.Base = &common.BaseReq{}
	kreq.UserType = parseUserType(req["user_type"])
	kreq.PublicKey = stringFromIface(req["public_key"])
	kreq.WalletAddress = stringFromIface(req["wallet_address"])
	kreq.WalletId = stringFromIface(req["wallet_id"])
	kreq.WalletName = stringFromIface(req["wallet_name"])
	if meta := mergeMetadata(nil, req["metadata"]); len(meta) > 0 {
		kreq.Metadata = meta
	}
	resp, err := c.cli.RegisterDID(ctx, kreq)
	if err != nil {
		return nil, 500, 0, err
	}
	out := flattenDIDRegisterRPC(resp)
	if wid := stringFromIface(req["wallet_id"]); wid != "" {
		out["wallet_id"] = wid
	}
	if wn := stringFromIface(req["wallet_name"]); wn != "" {
		out["wallet_name"] = wn
	}
	return out, 200, int(resp.GetBase().GetCode()), nil
}

func (c *KitexDIDClient) VerifyDID(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	kreq := did_service.NewVerifySignatureRequest()
	kreq.Did = common.DID(stringFromIface(req["did"]))
	kreq.Message = stringFromIface(req["message"])
	kreq.Signature = stringFromIface(req["signature"])
	kreq.Timestamp = stringFromIface(req["timestamp"])
	if n := stringFromIface(req["nonce"]); n != "" {
		kreq.Nonce = &n
	}
	resp, err := c.cli.VerifySignature(ctx, kreq)
	if err != nil {
		return nil, 500, 0, err
	}
	return map[string]interface{}{
		"valid": resp.Valid,
		"base":  baseToMap(resp.GetBase()),
	}, 200, int(resp.GetBase().GetCode()), nil
}

func (c *KitexDIDClient) GetDID(ctx context.Context, did string) (map[string]interface{}, int, int, error) {
	kreq := did_service.NewGetDIDRequest()
	kreq.Did = common.DID(did)
	resp, err := c.cli.GetDID(ctx, kreq)
	if err != nil {
		return nil, 500, 0, err
	}
	if resp.GetBase().GetCode() != 0 {
		return map[string]interface{}{"base": baseToMap(resp.GetBase())}, 404, int(resp.GetBase().GetCode()), fmt.Errorf("%s", resp.GetBase().GetMessage())
	}
	return map[string]interface{}{
		"did":            string(resp.GetDid()),
		"public_key":     resp.GetPublicKey(),
		"wallet_address": resp.GetWalletAddress(),
		"base":           baseToMap(resp.GetBase()),
	}, 200, int(resp.GetBase().GetCode()), nil
}

func (c *KitexDIDClient) VerifySignature(ctx context.Context, req map[string]interface{}) (bool, error) {
	kreq := did_service.NewVerifySignatureRequest()
	kreq.Did = common.DID(stringFromIface(req["did"]))
	kreq.Message = stringFromIface(req["message"])
	kreq.Signature = stringFromIface(req["signature"])
	kreq.Timestamp = stringFromIface(req["timestamp"])
	if n := stringFromIface(req["nonce"]); n != "" {
		kreq.Nonce = &n
	}
	resp, err := c.cli.VerifySignature(ctx, kreq)
	if err != nil {
		return false, err
	}
	return resp.GetValid(), nil
}

func baseToMap(b *common.BaseResp) map[string]interface{} {
	if b == nil {
		return map[string]interface{}{"code": float64(0), "message": "success"}
	}
	return map[string]interface{}{
		"code":    float64(b.GetCode()),
		"message": b.GetMessage(),
	}
}

func flattenDIDCreateRPC(resp *did_service.CreateDIDResponse) map[string]interface{} {
	return map[string]interface{}{
		"did":            string(resp.GetDid()),
		"public_key":     resp.GetPublicKey(),
		"wallet_address": resp.GetWalletAddress(),
		"created_at":     resp.GetCreatedAt(),
		"base":           baseToMap(resp.GetBase()),
	}
}

func flattenDIDRegisterRPC(resp *did_service.RegisterDIDResponse) map[string]interface{} {
	return map[string]interface{}{
		"did":            string(resp.GetDid()),
		"public_key":     resp.GetPublicKey(),
		"wallet_address": resp.GetWalletAddress(),
		"created_at":     resp.GetCreatedAt(),
		"base":           baseToMap(resp.GetBase()),
	}
}

func parseUserType(v interface{}) did_service.UserType {
	switch x := v.(type) {
	case string:
		s := strings.ToUpper(strings.TrimSpace(x))
		switch s {
		case "DEVELOPER", "DEV":
			return did_service.UserType_DEVELOPER
		case "AGENT", "":
			return did_service.UserType_AGENT
		default:
			if ut, err := did_service.UserTypeFromString(s); err == nil {
				return ut
			}
			return did_service.UserType_AGENT
		}
	case float64:
		if int64(x) == int64(did_service.UserType_DEVELOPER) {
			return did_service.UserType_DEVELOPER
		}
		return did_service.UserType_AGENT
	case int:
		if x == int(did_service.UserType_DEVELOPER) {
			return did_service.UserType_DEVELOPER
		}
		return did_service.UserType_AGENT
	case int64:
		if x == int64(did_service.UserType_DEVELOPER) {
			return did_service.UserType_DEVELOPER
		}
		return did_service.UserType_AGENT
	default:
		return did_service.UserType_AGENT
	}
}

func mergeMetadata(dst map[string]string, raw interface{}) map[string]string {
	if dst == nil {
		dst = make(map[string]string)
	}
	m, ok := raw.(map[string]interface{})
	if !ok || m == nil {
		return dst
	}
	for k, v := range m {
		if v == nil {
			continue
		}
		switch t := v.(type) {
		case string:
			dst[k] = t
		default:
			dst[k] = fmt.Sprintf("%v", t)
		}
	}
	return dst
}

func putIfStr(m map[string]string, key string, v interface{}) map[string]string {
	if m == nil {
		m = make(map[string]string)
	}
	s := stringFromIface(v)
	if s != "" {
		m[key] = s
	}
	return m
}

func stringFromIface(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}
