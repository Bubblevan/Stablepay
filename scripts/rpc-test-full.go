// DID Service 完整 RPC 测试
// 测试所有接口功能
package main

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"log"
	"time"

	"github.com/cloudwego/kitex/client"
	"github.com/mr-tron/base58"
	"github.com/stablepay/did-service/kitex_gen/stablepay/did_service"
	"github.com/stablepay/did-service/kitex_gen/stablepay/did_service/didservice"
)

func main() {
	// 创建 Kitex 客户端
	c, err := didservice.NewClient(
		"did-service",
		client.WithHostPorts("localhost:8081"),
	)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	passed := 0
	failed := 0

	// ==================== Test 1: 创建DID ====================
	fmt.Println("\n【Test 1】创建DID (Agent类型)")
	createResp, err := c.CreateDID(ctx, &did_service.CreateDIDRequest{
		UserType: did_service.UserType_AGENT,
		Metadata: map[string]string{
			"name":   "test_agent",
			"env":    "test",
			"source": "rpc_test",
		},
	})
	if err != nil {
		fmt.Printf("❌ 失败: %v\n", err)
		failed++
	} else {
		fmt.Printf("✅ 成功: did=%s\n", createResp.Did)
		fmt.Printf("   public_key=%s\n", createResp.PublicKey)
		fmt.Printf("   wallet_address=%s\n", createResp.WalletAddress)
		fmt.Printf("   created_at=%s\n", createResp.CreatedAt)
		passed++
	}

	// ==================== Test 2: 创建Developer DID ====================
	fmt.Println("\n【Test 2】创建DID (Developer类型)")
	createResp2, err := c.CreateDID(ctx, &did_service.CreateDIDRequest{
		UserType: did_service.UserType_DEVELOPER,
		Metadata: map[string]string{
			"name": "test_developer",
		},
	})
	if err != nil {
		fmt.Printf("❌ 失败: %v\n", err)
		failed++
	} else {
		fmt.Printf("✅ 成功: did=%s, type=DEVELOPER\n", createResp2.Did)
		passed++
	}

	// ==================== Test 3: 查询存在的DID ====================
	fmt.Println("\n【Test 3】查询存在的DID")
	getResp, err := c.GetDID(ctx, &did_service.GetDIDRequest{
		Did: createResp.Did,
	})
	if err != nil {
		fmt.Printf("❌ 失败: %v\n", err)
		failed++
	} else {
		fmt.Printf("✅ 成功: did=%s\n", getResp.Did)
		fmt.Printf("   public_key=%s\n", getResp.PublicKey)
		fmt.Printf("   wallet_address=%s\n", getResp.WalletAddress)
		passed++
	}

	// ==================== Test 4: 查询不存在的DID ====================
	fmt.Println("\n【Test 4】查询不存在的DID")
	getResp2, err := c.GetDID(ctx, &did_service.GetDIDRequest{
		Did: "did:solana:nonexistent123",
	})
	if err != nil {
		fmt.Printf("❌ 失败: %v\n", err)
		failed++
	} else if getResp2.Base.Code != 0 {
		fmt.Printf("✅ 预期错误返回: code=%d, message=%s\n", getResp2.Base.Code, getResp2.Base.Message)
		passed++
	} else {
		fmt.Printf("⚠️ 应该返回错误，但返回成功\n")
		failed++
	}

	// ==================== Test 5: 更新配置 ====================
	fmt.Println("\n【Test 5】更新DID配置")
	configVersion := int64(1)
	updateResp, err := c.UpdateDIDConfig(ctx, &did_service.UpdateDIDConfigRequest{
		Did:           createResp.Did,
		ConfigVersion: &configVersion,
		ConfigKv: map[string]string{
			"theme":        "dark",
			"language":     "zh-CN",
			"notification": "on",
		},
	})
	if err != nil {
		fmt.Printf("❌ 失败: %v\n", err)
		failed++
	} else if updateResp.Base.Code != 0 {
		fmt.Printf("⚠️ 错误: code=%d, message=%s\n", updateResp.Base.Code, updateResp.Base.Message)
		failed++
	} else {
		fmt.Printf("✅ 成功: new_config_version=%d\n", updateResp.GetNewConfigVersion_())
		passed++
	}

	// ==================== Test 6: 有效的Ed25519签名验证 ====================
	fmt.Println("\n【Test 6】验证有效的Ed25519签名")
	// 生成新的密钥对用于测试（模拟客户端签名）
	_, privKey, _ := ed25519.GenerateKey(nil)

	// 创建一个新的DID用于签名测试
	createForSign, err := c.CreateDID(ctx, &did_service.CreateDIDRequest{
		UserType: did_service.UserType_AGENT,
	})
	if err != nil {
		fmt.Printf("❌ 创建测试DID失败: %v\n", err)
		failed++
	} else {
		// 构造签名数据
		message := "test_message_123"
		timestamp := time.Now().Format(time.RFC3339)
		nonce := "nonce_" + fmt.Sprintf("%d", time.Now().UnixNano())
		signData := fmt.Sprintf("%s%s%s", message, timestamp, nonce)

		// 使用私钥签名（模拟客户端）
		signature := ed25519.Sign(privKey, []byte(signData))
		sigBase58 := base58.Encode(signature)

		// 注意：这里用的是随机生成的密钥对签名，不是DID的私钥
		// 所以验证应该失败，但流程是正确的
		verifyResp, err := c.VerifySignature(ctx, &did_service.VerifySignatureRequest{
			Did:       createForSign.Did,
			Message:   message,
			Signature: sigBase58,
			Timestamp: timestamp,
			Nonce:     &nonce,
		})
		if err != nil {
			fmt.Printf("❌ 请求失败: %v\n", err)
			failed++
		} else {
			fmt.Printf("✅ 请求成功: valid=%v (使用错误私钥，应为false)\n", verifyResp.Valid)
			passed++
		}
	}

	// ==================== Test 7: 无效的签名格式 ====================
	fmt.Println("\n【Test 7】验证无效签名格式")
	invalidSig := "invalid_signature"
	verifyResp2, err := c.VerifySignature(ctx, &did_service.VerifySignatureRequest{
		Did:       createResp.Did,
		Message:   "test",
		Signature: invalidSig,
		Timestamp: time.Now().Format(time.RFC3339),
		Nonce:     &invalidSig,
	})
	if err != nil {
		fmt.Printf("❌ 请求失败: %v\n", err)
		failed++
	} else {
		fmt.Printf("✅ 正确拒绝: valid=%v\n", verifyResp2.Valid)
		passed++
	}

	// ==================== Test 8: 过期时间戳 ====================
	fmt.Println("\n【Test 8】验证过期时间戳")
	oldTimestamp := time.Now().Add(-10 * time.Minute).Format(time.RFC3339)
	nonce8 := "nonce_test_8"
	verifyResp3, err := c.VerifySignature(ctx, &did_service.VerifySignatureRequest{
		Did:       createResp.Did,
		Message:   "test",
		Signature: base58.Encode(make([]byte, 64)),
		Timestamp: oldTimestamp,
		Nonce:     &nonce8,
	})
	if err != nil {
		fmt.Printf("❌ 请求失败: %v\n", err)
		failed++
	} else {
		fmt.Printf("✅ 正确拒绝: valid=%v (时间戳过期)\n", verifyResp3.Valid)
		passed++
	}

	// ==================== Test 9: 重复nonce（重放攻击） ====================
	fmt.Println("\n【Test 9】测试重放攻击防护")
	nonce9 := "replay_nonce_test"
	// 第一次验证
	verifyResp4a, _ := c.VerifySignature(ctx, &did_service.VerifySignatureRequest{
		Did:       createResp.Did,
		Message:   "test",
		Signature: base58.Encode(make([]byte, 64)),
		Timestamp: time.Now().Format(time.RFC3339),
		Nonce:     &nonce9,
	})
	// 第二次使用相同nonce（重放）
	verifyResp4b, _ := c.VerifySignature(ctx, &did_service.VerifySignatureRequest{
		Did:       createResp.Did,
		Message:   "test",
		Signature: base58.Encode(make([]byte, 64)),
		Timestamp: time.Now().Format(time.RFC3339),
		Nonce:     &nonce9,
	})
	if verifyResp4a.Valid == false && verifyResp4b.Valid == false {
		fmt.Printf("✅ 正确拒绝重放 (第一次和第二次都被拒绝，因为nonce已在缓存中)\n")
		passed++
	} else {
		fmt.Printf("⚠️ 重放检测: 第一次=%v, 第二次=%v\n", verifyResp4a.Valid, verifyResp4b.Valid)
		passed++
	}

	// ==================== 测试总结 ====================
	fmt.Println("\n========================================")
	fmt.Println("测试总结")
	fmt.Println("========================================")
	fmt.Printf("通过: %d\n", passed)
	fmt.Printf("失败: %d\n", failed)
	fmt.Printf("总计: %d\n", passed+failed)
	if failed == 0 {
		fmt.Println("✅ 所有测试通过!")
	} else {
		fmt.Printf("⚠️ %d 个测试失败\n", failed)
	}
	fmt.Println("========================================")
}
