package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
)

// M0 版本：最小化 x402 支付流程
// 核心：
// 1. /protected - 需要支付的资源
// 2. /pay - 处理支付
// 3. /verify - 验证购买关系

// PaymentRecord 记录购买关系
type PaymentRecord struct {
	AgentDID string `json:"agent_did"`
	SkillDID string `json:"skill_did"`
	Amount   int    `json:"amount"` // 单位：USDC/最小单位
	TxHash   string `json:"tx_hash"`
}

// PaymentRequirement x402 标准支付要求
type PaymentRequirement struct {
	Recipient   string `json:"recipient"`
	Amount      int    `json:"amount"`
	Currency    string `json:"currency"`
	Description string `json:"description"`
	Timeout     int    `json:"timeout"` // 秒
}

// PaymentProof 支付通知
type PaymentProof struct {
	AgentDID string `json:"agent_did"`
	TxHash   string `json:"tx_hash"`
	Amount   int    `json:"amount"`
}

// VerifyRequest 验证请求
type VerifyRequest struct {
	AgentDID string `json:"agent_did"`
	SkillDID string `json:"skill_did"`
}

// VerifyResponse 验证响应
type VerifyResponse struct {
	Verified bool   `json:"verified"`
	Message  string `json:"message"`
}

var (
	// M0: 简单的内存存储（实际生产要用数据库）
	payments  = make(map[string]PaymentRecord)
	paymentMu sync.Mutex

	// 配置常量
	SKILL_DID       = "did:skill:stablepay:v1"
	PAYMENT_WALLET  = "4zMMUHCXxYNbtjS7MBvVh8G7zVqKjn6fKgcR88VVFBq" // Solana 示例
	PAYMENT_AMOUNT  = 10000                                         // 0.01 USDC (6 decimals)
	PAYMENT_TIMEOUT = 300                                           // 5分钟
)

// handleProtected 需要支付的资源端点
// 逻辑：
// 1. 检查是否已支付 → 返回 200 + 内容
// 2. 未支付 → 返回 402 + 支付要求
func handleProtected(w http.ResponseWriter, r *http.Request) {
	agentDID := r.Header.Get("X-Agent-DID")
	if agentDID == "" {
		http.Error(w, `{"error":"Missing X-Agent-DID header"}`, http.StatusBadRequest)
		return
	}

	paymentMu.Lock()
	record, exists := payments[agentDID]
	paymentMu.Unlock()

	// 已支付
	if exists && record.SkillDID == SKILL_DID {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "ok",
			"data":    "This is paid premium skill content. Agent: " + agentDID,
			"paid_at": record,
		})
		return
	}

	// 未支付 → 返回 402
	requirement := PaymentRequirement{
		Recipient:   PAYMENT_WALLET,
		Amount:      PAYMENT_AMOUNT,
		Currency:    "USDC",
		Description: "Payment required for StablePay Skill access",
		Timeout:     PAYMENT_TIMEOUT,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Payment-Recipient", PAYMENT_WALLET)
	w.Header().Set("X-Payment-Amount", fmt.Sprintf("%d", PAYMENT_AMOUNT))
	w.Header().Set("X-Payment-Currency", "USDC")
	w.WriteHeader(http.StatusPaymentRequired) // 402
	json.NewEncoder(w).Encode(requirement)
}

// handlePay 客户端支付后调用此接口
// 在 M0 中，我们直接记录（实际需要验证 Solana 链上交易）
func handlePay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var proof PaymentProof
	if err := json.NewDecoder(r.Body).Decode(&proof); err != nil {
		http.Error(w, `{"error":"Invalid payment proof"}`, http.StatusBadRequest)
		return
	}

	// M0: 验证逻辑简化（实际需要调用 Solana RPC 验证 tx_hash）
	if proof.AgentDID == "" || proof.TxHash == "" {
		http.Error(w, `{"error":"Missing agent_did or tx_hash"}`, http.StatusBadRequest)
		return
	}

	paymentMu.Lock()
	payments[proof.AgentDID] = PaymentRecord{
		AgentDID: proof.AgentDID,
		SkillDID: SKILL_DID,
		Amount:   proof.Amount,
		TxHash:   proof.TxHash,
	}
	paymentMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "payment_recorded",
		"message": "Payment recorded successfully. Please retry the request.",
	})
}

// handleVerify 验证某个 Agent 是否已支付
func handleVerify(w http.ResponseWriter, r *http.Request) {
	var req VerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request"}`, http.StatusBadRequest)
		return
	}

	paymentMu.Lock()
	record, exists := payments[req.AgentDID]
	paymentMu.Unlock()

	response := VerifyResponse{
		Verified: exists && record.SkillDID == req.SkillDID,
	}

	if response.Verified {
		response.Message = "Agent has valid payment"
	} else {
		response.Message = "No valid payment found for this agent"
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// handleHealth 健康检查
func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func main() {
	http.HandleFunc("/health", handleHealth)
	http.HandleFunc("/protected", handleProtected)
	http.HandleFunc("/pay", handlePay)
	http.HandleFunc("/verify", handleVerify)

	port := "8080"
	log.Printf("StablePay M0 Server listening on :%s\n", port)
	log.Printf("Skill DID: %s\n", SKILL_DID)

	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}
