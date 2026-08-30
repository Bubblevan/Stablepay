// DID Service RPC 测试客户端
// 用于替代 HTTP curl 测试
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/cloudwego/kitex/client"
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

	// 1. 测试创建 DID
	fmt.Println("=== 测试创建DID ===")
	createResp, err := c.CreateDID(ctx, &did_service.CreateDIDRequest{
		UserType: did_service.UserType_AGENT,
		Metadata: map[string]string{
			"name": "test_user",
		},
	})
	if err != nil {
		log.Fatalf("创建DID失败: %v", err)
	}
	fmt.Printf("创建成功: did=%s, public_key=%s\n", createResp.Did, createResp.PublicKey)

	// 2. 测试查询 DID
	fmt.Println("\n=== 测试查询DID ===")
	getResp, err := c.GetDID(ctx, &did_service.GetDIDRequest{
		Did: createResp.Did,
	})
	if err != nil {
		log.Fatalf("查询DID失败: %v", err)
	}
	fmt.Printf("查询成功: did=%s, status=%s\n", getResp.Did, getResp.Base.Message)

	// 3. 测试签名验证（简化版，无真实签名）
	fmt.Println("\n=== 测试签名验证 ===")
	nonce := "test_nonce"
	verifyResp, err := c.VerifySignature(ctx, &did_service.VerifySignatureRequest{
		Did:       createResp.Did,
		Message:   "test message",
		Signature: "invalid_signature_for_test",
		Timestamp: time.Now().Format(time.RFC3339),
		Nonce:     &nonce,
	})
	if err != nil {
		log.Fatalf("验证签名失败: %v", err)
	}
	fmt.Printf("验证结果: valid=%v\n", verifyResp.Valid)

	fmt.Println("\n=== 所有测试通过 ===")
}
