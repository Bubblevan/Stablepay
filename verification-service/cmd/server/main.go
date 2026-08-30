package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
)

// M0 �汾����С�� x402 ֧������
// ���ģ�
// 1. /protected - ��Ҫ֧������Դ
// 2. /pay - ����֧��
// 3. /verify - ��֤�����ϵ

// PaymentRecord ��¼�����ϵ
type PaymentRecord struct {
	AgentDID string `json:"agent_did"`
	SkillDID string `json:"skill_did"`
	Amount   int    `json:"amount"` // ��λ��USDC/��С��λ
	TxHash   string `json:"tx_hash"`
}

// PaymentRequirement x402 ��׼֧��Ҫ��
type PaymentRequirement struct {
	Recipient   string `json:"recipient"`
	Amount      int    `json:"amount"`
	Currency    string `json:"currency"`
	Description string `json:"description"`
	Timeout     int    `json:"timeout"` // ��
}

// PaymentProof ֧��֪ͨ
type PaymentProof struct {
	AgentDID string `json:"agent_did"`
	TxHash   string `json:"tx_hash"`
	Amount   int    `json:"amount"`
}

// VerifyRequest ��֤����
type VerifyRequest struct {
	AgentDID string `json:"agent_did"`
	SkillDID string `json:"skill_did"`
}

// VerifyResponse ��֤��Ӧ
type VerifyResponse struct {
	Verified bool   `json:"verified"`
	Message  string `json:"message"`
}

var (
	// M0: �򵥵��ڴ�洢��ʵ������Ҫ�����ݿ⣩
	payments  = make(map[string]PaymentRecord)
	paymentMu sync.Mutex

	// ���ó���
	SKILL_DID       = "did:skill:stablepay:v1"
	PAYMENT_WALLET  = "4zMMUHCXxYNbtjS7MBvVh8G7zVqKjn6fKgcR88VVFBq" // Solana ʾ��
	PAYMENT_AMOUNT  = 10000                                         // 0.01 USDC (6 decimals)
	PAYMENT_TIMEOUT = 300                                           // 5����
)

// handleProtected ��Ҫ֧������Դ�˵�
// �߼���
// 1. ����Ƿ���֧�� �� ���� 200 + ����
// 2. δ֧�� �� ���� 402 + ֧��Ҫ��
func handleProtected(w http.ResponseWriter, r *http.Request) {
	agentDID := r.Header.Get("X-Agent-DID")
	if agentDID == "" {
		http.Error(w, `{"error":"Missing X-Agent-DID header"}`, http.StatusBadRequest)
		return
	}

	paymentMu.Lock()
	record, exists := payments[agentDID]
	paymentMu.Unlock()

	// ��֧��
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

	// δ֧�� �� ���� 402
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

// handlePay �ͻ���֧������ô˽ӿ�
// �� M0 �У�����ֱ�Ӽ�¼��ʵ����Ҫ��֤ Solana ���Ͻ��ף�
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

	// M0: ��֤�߼��򻯣�ʵ����Ҫ���� Solana RPC ��֤ tx_hash��
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

// handleVerify ��֤ĳ�� Agent �Ƿ���֧��
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

// handleHealth �������
func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func main() {
	http.HandleFunc("/health", handleHealth)
	http.HandleFunc("/protected", handleProtected)
	http.HandleFunc("/pay", handlePay)
	http.HandleFunc("/verify", handleVerify)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8085"
	}
	log.Printf("StablePay M0 Server listening on :%s\n", port)
	log.Printf("Skill DID: %s\n", SKILL_DID)

	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}
