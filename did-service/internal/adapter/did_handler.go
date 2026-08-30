// Package adapter RPC Handler层
// COLA v5: Adapter Layer
// 接收RPC请求，转换为应用服务调用
package adapter

import (
	"context"
	"strings"

	"github.com/stablepay/did-service/internal/application"
	"github.com/stablepay/did-service/kitex_gen/stablepay/common"
	"github.com/stablepay/did-service/kitex_gen/stablepay/did_service"
)

// DIDHandlerImpl DID RPC处理器实现
type DIDHandlerImpl struct {
	appService *app.DIDAppService
}

// NewDIDHandler 创建DID处理器
func NewDIDHandler(appService *app.DIDAppService) *DIDHandlerImpl {
	return &DIDHandlerImpl{appService: appService}
}

// CreateDID 创建DID
func (h *DIDHandlerImpl) CreateDID(ctx context.Context, req *did_service.CreateDIDRequest) (*did_service.CreateDIDResponse, error) {
	// 1. 参数校验
	if req == nil {
		return &did_service.CreateDIDResponse{
			Base: h.buildBaseResp(10001, "request is nil"),
		}, nil
	}

	// 2. 转换请求
	var userType app.UserType
	switch req.UserType {
	case did_service.UserType_AGENT:
		userType = app.UserTypeAgent
	case did_service.UserType_DEVELOPER:
		userType = app.UserTypeDeveloper
	default:
		userType = app.UserTypeAgent
	}

	cmd := &app.CreateDIDCmd{
		UserType: userType,
		Metadata: req.Metadata,
	}

	// 3. 调用应用服务
	result, err := h.appService.CreateDID(ctx, cmd)
	if err != nil {
		return &did_service.CreateDIDResponse{
			Base: h.buildBaseResp(10002, err.Error()),
		}, nil
	}

	// 4. 构造响应
	return &did_service.CreateDIDResponse{
		Base:          h.buildBaseResp(0, "success"),
		Did:           result.DIDString,
		PublicKey:     result.PublicKey,
		WalletAddress: result.WalletAddress,
		CreatedAt:     result.CreatedAt,
	}, nil
}

// RegisterDID 绑定客户端已有钱包（不生成私钥）。
func (h *DIDHandlerImpl) RegisterDID(ctx context.Context, req *did_service.RegisterDIDRequest) (*did_service.RegisterDIDResponse, error) {
	if req == nil {
		return &did_service.RegisterDIDResponse{
			Base: h.buildBaseResp(10001, "request is nil"),
		}, nil
	}

	var userType app.UserType
	switch req.UserType {
	case did_service.UserType_AGENT:
		userType = app.UserTypeAgent
	case did_service.UserType_DEVELOPER:
		userType = app.UserTypeDeveloper
	default:
		userType = app.UserTypeAgent
	}

	cmd := &app.RegisterDIDCmd{
		UserType:      userType,
		PublicKey:     req.PublicKey,
		WalletAddress: req.WalletAddress,
		WalletID:      req.WalletId,
		WalletName:    req.WalletName,
		Metadata:      req.Metadata,
	}

	result, err := h.appService.RegisterDID(ctx, cmd)
	if err != nil {
		return &did_service.RegisterDIDResponse{
			Base: h.buildBaseResp(registerDIDErrorCode(err.Error()), err.Error()),
		}, nil
	}

	return &did_service.RegisterDIDResponse{
		Base:          h.buildBaseResp(0, "success"),
		Did:           result.DIDString,
		PublicKey:     result.PublicKey,
		WalletAddress: result.WalletAddress,
		CreatedAt:     result.CreatedAt,
	}, nil
}

func registerDIDErrorCode(msg string) int32 {
	switch {
	case strings.Contains(msg, "public_key is required"),
		strings.Contains(msg, "invalid public_key"),
		strings.Contains(msg, "wallet_address must match"),
		strings.Contains(msg, "command is nil"):
		return 10001
	default:
		return 10002
	}
}

// GetDID 查询DID
func (h *DIDHandlerImpl) GetDID(ctx context.Context, req *did_service.GetDIDRequest) (*did_service.GetDIDResponse, error) {
	// 1. 参数校验
	if req == nil || req.Did == "" {
		return &did_service.GetDIDResponse{
			Base: h.buildBaseResp(10001, "request or did is empty"),
		}, nil
	}

	// 2. 转换请求
	query := &app.GetDIDQuery{
		DIDString: req.Did,
	}

	// 3. 调用应用服务
	result, err := h.appService.GetDID(ctx, query)
	if err != nil {
		return &did_service.GetDIDResponse{
			Base: h.buildBaseResp(10003, err.Error()),
		}, nil
	}

	// 4. 构造响应
	return &did_service.GetDIDResponse{
		Base:          h.buildBaseResp(0, "success"),
		Did:           result.DIDString,
		PublicKey:     result.PublicKey,
		WalletAddress: result.WalletAddress,
	}, nil
}

// VerifySignature 验证签名
func (h *DIDHandlerImpl) VerifySignature(ctx context.Context, req *did_service.VerifySignatureRequest) (*did_service.VerifySignatureResponse, error) {
	// 1. 参数校验
	if req == nil || req.Did == "" {
		return &did_service.VerifySignatureResponse{
			Base:  h.buildBaseResp(10001, "request or did is empty"),
			Valid: false,
		}, nil
	}

	// 2. 转换请求
	cmd := &app.VerifySignatureCmd{
		DIDString: req.Did,
		Message:   req.Message,
		Signature: req.Signature,
		Timestamp: req.Timestamp,
		Nonce:     req.GetNonce(),
	}

	// 3. 调用应用服务
	result, err := h.appService.VerifySignature(ctx, cmd)
	if err != nil {
		return &did_service.VerifySignatureResponse{
			Base:  h.buildBaseResp(10004, err.Error()),
			Valid: false,
		}, nil
	}

	// 4. 构造响应
	return &did_service.VerifySignatureResponse{
		Base:  h.buildBaseResp(0, "success"),
		Valid: result.Valid,
	}, nil
}

// UpdateDIDConfig 更新DID配置
func (h *DIDHandlerImpl) UpdateDIDConfig(ctx context.Context, req *did_service.UpdateDIDConfigRequest) (*did_service.UpdateDIDConfigResponse, error) {
	// 1. 参数校验
	if req == nil || req.Did == "" {
		return &did_service.UpdateDIDConfigResponse{
			Base: h.buildBaseResp(10001, "request or did is empty"),
		}, nil
	}

	// 2. 转换请求
	var version int64
	if req.IsSetConfigVersion() {
		version = req.GetConfigVersion()
	}

	cmd := &app.UpdateConfigCmd{
		DIDString:     req.Did,
		ConfigVersion: version,
		ConfigKV:      req.ConfigKv,
	}

	// 3. 调用应用服务
	result, err := h.appService.UpdateConfig(ctx, cmd)
	if err != nil {
		return &did_service.UpdateDIDConfigResponse{
			Base: h.buildBaseResp(10005, err.Error()),
		}, nil
	}

	// 4. 构造响应
	newVersion := result.NewConfigVersion
	return &did_service.UpdateDIDConfigResponse{
		Base:              h.buildBaseResp(0, "success"),
		NewConfigVersion_: &newVersion,
	}, nil
}

// buildBaseResp 构建基础响应
func (h *DIDHandlerImpl) buildBaseResp(code int32, message string) *common.BaseResp {
	return &common.BaseResp{
		Code:    code,
		Message: message,
	}
}
