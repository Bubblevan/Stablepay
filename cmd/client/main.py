#!/usr/bin/env python3
"""
M0 版本：Python CLI 客户端，模拟 OpenClaw 中的 Agent
核心逻辑：
1. 请求受保护资源 /protected
2. 如果收到 402，读取支付要求
3. 触发支付 → 调用 /pay
4. 重试请求 /protected → 应该返回 200
"""

import requests
import json
import sys
import time
import argparse
from typing import Optional, Dict, Any

# 全局配置
DEFAULT_SERVER = "http://localhost:8080"
DEFAULT_AGENT_DID = "did:agent:test:m0"  # 模拟的 Agent DID


class X402Client:
    """x402 协议客户端"""

    def __init__(self, server_url: str = DEFAULT_SERVER, agent_did: str = DEFAULT_AGENT_DID):
        self.server_url = server_url.rstrip("/")
        self.agent_did = agent_did

    def access_protected_resource(self) -> tuple[int, Dict[str, Any]]:
        """
        步骤 1：请求受保护资源
        返回: (status_code, response_json)
        """
        url = f"{self.server_url}/protected"
        headers = {
            "X-Agent-DID": self.agent_did,
        }

        print(f"[1] 请求受保护资源: {url}")
        print(f"    Agent DID: {self.agent_did}")

        try:
            resp = requests.get(url, headers=headers, timeout=5)
            return resp.status_code, resp.json()
        except Exception as e:
            print(f"    ? 请求失败: {e}")
            return 500, {"error": str(e)}

    def handle_402_payment(self, payment_requirement: Dict[str, Any]) -> bool:
        """
        步骤 2：处理 402 支付要求
        在 M0 中，我们模拟支付（生成一个 mock tx_hash）
        """
        print(f"\n[2] 收到 402 Payment Required")
        print(f"    支付要求: {json.dumps(payment_requirement, indent=2)}")

        # M0: 模拟支付
        # 实际场景: 需要调用 Solana SDK，用本地私钥签名并发送交易
        mock_tx_hash = "5mVz4n7kL2pQwR9xJ8tY3uA6bC1dE5fG7hI9jK0lM2nO3pQ4rStU5vW"

        print(f"\n[3] 执行支付 (M0 模拟)")
        print(f"    Mock TX Hash: {mock_tx_hash}")

        # 调用后端 /pay 接口
        payment_proof = {
            "agent_did": self.agent_did,
            "tx_hash": mock_tx_hash,
            "amount": payment_requirement.get("amount", 10000),
        }

        url = f"{self.server_url}/pay"
        try:
            resp = requests.post(url, json=payment_proof, timeout=5)
            if resp.status_code == 200:
                print(f"    ? 支付已记录")
                print(f"    响应: {resp.json()}")
                return True
            else:
                print(f"    ? 支付失败: {resp.status_code}")
                print(f"    {resp.text}")
                return False
        except Exception as e:
            print(f"    ? 请求失败: {e}")
            return False

    def retry_protected_resource(self) -> tuple[int, Dict[str, Any]]:
        """
        步骤 4：支付后重试请求受保护资源
        应该返回 200 OK
        """
        print(f"\n[4] 重试请求受保护资源...")
        time.sleep(0.5)  # 短暂延迟，让支付处理完成

        url = f"{self.server_url}/protected"
        headers = {
            "X-Agent-DID": self.agent_did,
        }

        try:
            resp = requests.get(url, headers=headers, timeout=5)
            return resp.status_code, resp.json()
        except Exception as e:
            print(f"    ? 请求失败: {e}")
            return 500, {"error": str(e)}

    def verify_payment(self) -> bool:
        """
        验证支付状态
        """
        print(f"\n[验证] 检查支付状态...")
        url = f"{self.server_url}/verify"
        payload = {
            "agent_did": self.agent_did,
            "skill_did": "did:skill:stablepay:v1",
        }

        try:
            resp = requests.post(url, json=payload, timeout=5)
            if resp.status_code == 200:
                result = resp.json()
                print(f"    验证结果: {result}")
                return result.get("verified", False)
        except Exception as e:
            print(f"    ? 验证失败: {e}")

        return False

    def run(self) -> bool:
        """
        运行完整的 x402 流程
        """
        print("=" * 60)
        print("StablePay M0 - x402 客户端演示")
        print("=" * 60)

        # 步骤 1: 请求受保护资源
        status, response = self.access_protected_resource()

        # 步骤 2: 处理 402
        if status == 402:
            print(f"    ? 收到 402，需要支付")

            # 步骤 3: 执行支付
            if not self.handle_402_payment(response):
                print("\n? 支付失败，中止")
                return False

            # 步骤 4: 重试
            status, response = self.retry_protected_resource()

        # 最终检查
        print(f"\n[结果] 最终状态码: {status}")
        if status == 200:
            print(f"    ? 成功获取受保护资源!")
            print(f"    响应: {json.dumps(response, indent=2)}")

            # 验证支付
            self.verify_payment()

            print("\n" + "=" * 60)
            print("? x402 流程完成")
            print("=" * 60)
            return True
        else:
            print(f"    ? 失败 ({status})")
            print(f"    {json.dumps(response, indent=2)}")
            return False


def main():
    parser = argparse.ArgumentParser(
        description="StablePay M0 x402 客户端"
    )
    parser.add_argument(
        "--server",
        default=DEFAULT_SERVER,
        help=f"服务端 URL (默认: {DEFAULT_SERVER})"
    )
    parser.add_argument(
        "--agent-did",
        default=DEFAULT_AGENT_DID,
        help=f"Agent DID (默认: {DEFAULT_AGENT_DID})"
    )

    args = parser.parse_args()

    client = X402Client(server_url=args.server, agent_did=args.agent_did)
    success = client.run()

    sys.exit(0 if success else 1)


if __name__ == "__main__":
    main()
