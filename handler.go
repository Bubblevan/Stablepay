package main

import (
	"context"
	"log"
	"verification-service/kitex_gen/stablepay/common"
	"verification-service/kitex_gen/stablepay/verification_service"
)


func strPtr(s string) *string {
	return &s
}// 由于go的构思特性，这个小工具函数把字符串转成指针，解决莫名其妙的编译报错

// VerificationServiceImpl implements the last service interface defined in the IDL.
type VerificationServiceImpl struct{}

// VerifyPurchase implements the VerificationServiceImpl interface.
func (s *VerificationServiceImpl) VerifyPurchase(ctx context.Context, req *verification_service.VerifyPurchaseRequest) (resp *verification_service.VerifyPurchaseResponse, err error) {

    log.Printf("👉 【服务端】收到请求参数: AgentDid='%s', SkillDid='%s'", req.AgentDid, req.SkillDid)

    resp = verification_service.NewVerifyPurchaseResponse()
    var record PurchaseRecord

    result := DB.Where("agent_did = ? AND skill_did = ?", req.AgentDid, req.SkillDid).First(&record)

    if result.Error == nil {
        log.Printf("【服务端】查到数据！真实流水号: %s", record.TxId)
        resp.Purchased = true
        resp.PurchaseTime = strPtr("2026-03-12T15:00:00Z")
        txId := common.TxId(record.TxId)
        resp.TxId = &txId
        resp.Base = &common.BaseResp{Code: 0, Message: "success"}
    } else {
        log.Printf("【服务端】数据库报错信息: %v", result.Error)
        resp.Purchased = false
        resp.Base = &common.BaseResp{Code: 10001, Message: "record not found"}
    }

    return resp, nil
}

// BatchVerifyPurchase implements the VerificationServiceImpl interface.
func (s *VerificationServiceImpl) BatchVerifyPurchase(ctx context.Context, req *verification_service.BatchVerifyPurchaseRequest) (resp *verification_service.BatchVerifyPurchaseResponse, err error) {
	resp = verification_service.NewBatchVerifyPurchaseResponse()
	return resp, nil
}

// GetPurchaseProof implements the VerificationServiceImpl interface.
func (s *VerificationServiceImpl) GetPurchaseProof(ctx context.Context, req *verification_service.GetPurchaseProofRequest) (resp *verification_service.GetPurchaseProofResponse, err error) {
	resp = verification_service.NewGetPurchaseProofResponse()
	return resp, nil
}