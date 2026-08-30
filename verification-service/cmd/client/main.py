#!/usr/bin/env python3
"""
M0 �汾��Python CLI �ͻ��ˣ�ģ�� OpenClaw �е� Agent
�����߼���
1. �����ܱ�����Դ /protected
2. ����յ� 402����ȡ֧��Ҫ��
3. ����֧�� �� ���� /pay
4. �������� /protected �� Ӧ�÷��� 200
"""

import requests
import json
import sys
import time
import argparse
from typing import Optional, Dict, Any

# ȫ������
DEFAULT_SERVER = "http://localhost:8080"
DEFAULT_AGENT_DID = "did:agent:test:m0"  # ģ��� Agent DID


class X402Client:
    """x402 Э��ͻ���"""

    def __init__(self, server_url: str = DEFAULT_SERVER, agent_did: str = DEFAULT_AGENT_DID):
        self.server_url = server_url.rstrip("/")
        self.agent_did = agent_did

    def access_protected_resource(self) -> tuple[int, Dict[str, Any]]:
        """
        ���� 1�������ܱ�����Դ
        ����: (status_code, response_json)
        """
        url = f"{self.server_url}/protected"
        headers = {
            "X-Agent-DID": self.agent_did,
        }

        print(f"[1] �����ܱ�����Դ: {url}")
        print(f"    Agent DID: {self.agent_did}")

        try:
            resp = requests.get(url, headers=headers, timeout=5)
            return resp.status_code, resp.json()
        except Exception as e:
            print(f"    ? ����ʧ��: {e}")
            return 500, {"error": str(e)}

    def handle_402_payment(self, payment_requirement: Dict[str, Any]) -> bool:
        """
        ���� 2������ 402 ֧��Ҫ��
        �� M0 �У�����ģ��֧��������һ�� mock tx_hash��
        """
        print(f"\n[2] �յ� 402 Payment Required")
        print(f"    ֧��Ҫ��: {json.dumps(payment_requirement, indent=2)}")

        # M0: ģ��֧��
        # ʵ�ʳ���: ��Ҫ���� Solana SDK���ñ���˽Կǩ�������ͽ���
        mock_tx_hash = "5mVz4n7kL2pQwR9xJ8tY3uA6bC1dE5fG7hI9jK0lM2nO3pQ4rStU5vW"

        print(f"\n[3] ִ��֧�� (M0 ģ��)")
        print(f"    Mock TX Hash: {mock_tx_hash}")

        # ���ú�� /pay �ӿ�
        payment_proof = {
            "agent_did": self.agent_did,
            "tx_hash": mock_tx_hash,
            "amount": payment_requirement.get("amount", 10000),
        }

        url = f"{self.server_url}/pay"
        try:
            resp = requests.post(url, json=payment_proof, timeout=5)
            if resp.status_code == 200:
                print(f"    ? ֧���Ѽ�¼")
                print(f"    ��Ӧ: {resp.json()}")
                return True
            else:
                print(f"    ? ֧��ʧ��: {resp.status_code}")
                print(f"    {resp.text}")
                return False
        except Exception as e:
            print(f"    ? ����ʧ��: {e}")
            return False

    def retry_protected_resource(self) -> tuple[int, Dict[str, Any]]:
        """
        ���� 4��֧�������������ܱ�����Դ
        Ӧ�÷��� 200 OK
        """
        print(f"\n[4] ���������ܱ�����Դ...")
        time.sleep(0.5)  # �����ӳ٣���֧���������

        url = f"{self.server_url}/protected"
        headers = {
            "X-Agent-DID": self.agent_did,
        }

        try:
            resp = requests.get(url, headers=headers, timeout=5)
            return resp.status_code, resp.json()
        except Exception as e:
            print(f"    ? ����ʧ��: {e}")
            return 500, {"error": str(e)}

    def verify_payment(self) -> bool:
        """
        ��֤֧��״̬
        """
        print(f"\n[��֤] ���֧��״̬...")
        url = f"{self.server_url}/verify"
        payload = {
            "agent_did": self.agent_did,
            "skill_did": "did:skill:stablepay:v1",
        }

        try:
            resp = requests.post(url, json=payload, timeout=5)
            if resp.status_code == 200:
                result = resp.json()
                print(f"    ��֤���: {result}")
                return result.get("verified", False)
        except Exception as e:
            print(f"    ? ��֤ʧ��: {e}")

        return False

    def run(self) -> bool:
        """
        ���������� x402 ����
        """
        print("=" * 60)
        print("StablePay M0 - x402 �ͻ�����ʾ")
        print("=" * 60)

        # ���� 1: �����ܱ�����Դ
        status, response = self.access_protected_resource()

        # ���� 2: ���� 402
        if status == 402:
            print(f"    ? �յ� 402����Ҫ֧��")

            # ���� 3: ִ��֧��
            if not self.handle_402_payment(response):
                print("\n? ֧��ʧ�ܣ���ֹ")
                return False

            # ���� 4: ����
            status, response = self.retry_protected_resource()

        # ���ռ��
        print(f"\n[���] ����״̬��: {status}")
        if status == 200:
            print(f"    ? �ɹ���ȡ�ܱ�����Դ!")
            print(f"    ��Ӧ: {json.dumps(response, indent=2)}")

            # ��֤֧��
            self.verify_payment()

            print("\n" + "=" * 60)
            print("? x402 �������")
            print("=" * 60)
            return True
        else:
            print(f"    ? ʧ�� ({status})")
            print(f"    {json.dumps(response, indent=2)}")
            return False


def main():
    parser = argparse.ArgumentParser(
        description="StablePay M0 x402 �ͻ���"
    )
    parser.add_argument(
        "--server",
        default=DEFAULT_SERVER,
        help=f"����� URL (Ĭ��: {DEFAULT_SERVER})"
    )
    parser.add_argument(
        "--agent-did",
        default=DEFAULT_AGENT_DID,
        help=f"Agent DID (Ĭ��: {DEFAULT_AGENT_DID})"
    )

    args = parser.parse_args()

    client = X402Client(server_url=args.server, agent_did=args.agent_did)
    success = client.run()

    sys.exit(0 if success else 1)


if __name__ == "__main__":
    main()
