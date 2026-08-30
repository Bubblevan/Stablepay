package main

import (
	"context"
	"fmt"
	"log"

	"github.com/cloudwego/kitex/client"
	// 注意检查下面的路径是否和你的 kitex_gen 目录匹配
	"github.com/stablepay/verification-service/kitex_gen/stablepay/verification_service"
	"github.com/stablepay/verification-service/kitex_gen/stablepay/verification_service/verificationservice"
)

func main() {

	cli, err := verificationservice.NewClient("verification-service", client.WithHostPorts("127.0.0.1:8085"))
	if err != nil {
		log.Fatal("创建客户端失败:", err)
	}

	fmt.Println("开始测试 Verification Service 接口")

	fmt.Println("\n[测试] VerifyPurchase...")
	req := &verification_service.VerifyPurchaseRequest{
		AgentDid: "did:agent:test_123",
		SkillDid: "did:developer:skill_456",
	}

	resp, err := cli.VerifyPurchase(context.Background(), req)
	if err != nil {
		log.Fatal("调用失败:", err)
	}

	fmt.Printf("验证调用成功！返回状态: Code = %d, Message = %s\n", resp.Base.Code, resp.Base.Message)
}
