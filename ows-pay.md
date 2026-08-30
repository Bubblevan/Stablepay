## 0）创建 OWS 钱包并理解角色映射
这份手册对应的是「买家真实签名转账 + hotwallet 仅补 fee payer 签名」模式。

在开始前，先明确 3 个角色：
- 买家钱包（Agent）：`WALLET_AGENT`，用于业务签名 + 交易签名（Solana 交易签名走 `ows sign message --encoding hex` 对 message bytes 签名）。
- 卖家钱包（Skill）：建议每次“新商品联调”创建一个新钱包，映射成新的 `NEW_SKILL_DID`。
- 平台热钱包（Hotwallet）：服务端配置在 `blockchain-adapter/conf/hotwallet.json`，只用于补 fee payer，不代表买家资产。

先在 WSL 安装 OWS，并创建买家钱包（示例）：
```bash
ows wallet create --name "stablepay-agent"
Wallet created: 071ccfe1-c6c4-4b4c-bbad-274033f17c61
Name:           stablepay-agent

  eip155:1 → 0x9a1C3803Fb4A68c8824a82fc9013D29d8617327C
    Path: m/44'/60'/0'/0/0
  solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp → 2gL5tHKBp2ZWGX9chgcHFtEqLcwG2GT76CimizqFqXXF
    Path: m/44'/501'/0'/0'
  bip122:000000000019d6689c085ae165831e93 → bc1qsx3wd82w0kxrdd2m9vuvr9da5a69fz5tvjetsr
    Path: m/84'/0'/0'/0/0
  cosmos:cosmoshub-4 → cosmos1uhvn05q75u0ztl7z6q2aed4n7vr655r7ytn53l
    Path: m/44'/118'/0'/0/0
  tron:mainnet → TBRE2qKnLfyzdzidCrJsdt9faJAcLfmbyY
    Path: m/44'/195'/0'/0/0
  ton:mainnet → UQAxP_vfrDIVTgOarHJdGYtpgGkEax5jvNHLGo026g7NljnN
    Path: m/44'/607'/0'
  fil:mainnet → f1remfbhpg6srbf76lp5j24lbnlmkm7gscdr7a4aq
    Path: m/44'/461'/0'/0/0
  sui:mainnet → 0xb342597547f0a788d74761f7e10bd3623614660325999f3f0325a5e91ea27e78
    Path: m/44'/784'/0'/0'/0'

Mnemonic encrypted and saved to vault.
Use --show-mnemonic at creation time if you need a backup copy.
```

OWS `wallet create` 会一次性派生多链地址。这里重点看 Solana：
- `Wallet created: ...`：OWS 钱包 ID（UUID），不是链上地址。
- `solana:...`：CAIP-2 链标识，不是地址。
- `→ 2gL5...`：这才是 Solana 公钥/地址（Base58）。
- `Path: m/44'/501'/0'/0'`：派生路径。

协作建议：
- 真实联调时把「钱包名 + 钱包ID + Solana地址 + DID」记录到团队共享文档，避免不同人混用变量。
- 每次删卷重启后，数据库会清空，需要重做 DID 注册（钱包本地不丢）。

## 1）用单行 python -c + requests 查链上 USDC 余额
先确认 3 个地址的链上余额（devnet USDC）：
- 买家地址（`AGENT_SOL`）应有可支付余额。
- 卖家地址（新建 `NEW_SKILL_SOL`）初始通常为 0。
- hotwallet 地址用于 fee payer，需有足够 SOL（和可能的代币出入账观测）。

记得先在 devnet 准备好 USDC（示例）：
```bash
OWNER="2gL5tHKBp2ZWGX9chgcHFtEqLcwG2GT76CimizqFqXXF" python3 -c 'import os,requests,json; rpc="https://api.devnet.solana.com"; owner=os.environ["OWNER"]; mint="4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU"; payload={"jsonrpc":"2.0","id":1,"method":"getTokenAccountsByOwner","params":[owner,{"mint":mint},{"encoding":"jsonParsed"}]}; resp=requests.post(rpc,json=payload,timeout=20); v=resp.json().get("result",{}).get("value",[]); total=sum(float(x["account"]["data"]["parsed"]["info"]["tokenAmount"].get("uiAmountString","0")) for x in v); print(json.dumps({"owner":owner,"balance":total,"token_accounts":len(v)}))'

{"owner": "2gL5tHKBp2ZWGX9chgcHFtEqLcwG2GT76CimizqFqXXF", "balance": 40.0, "token_accounts": 1}
```
当然也可以
```bash
spl-token accounts --owner 2gL5tHKBp2ZWGX9chgcHFtEqLcwG2GT76CimizqFqXXF
Token                                         Balance
-----------------------------------------------------
4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU  40
```
还有hotwallet手续费：
```bash
OWNER="FMNs7xqezz4bYYioPyfqPzxLLmZyJhjSzbGApMdnrC2Z" python3 -c 'import os,requests,json; rpc="https://api.devnet.solana.com"; owner=os.environ["OWNER"]; mint="4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU"; payload={"jsonrpc":"2.0","id":1,"method":"getTokenAccountsByOwner","params":[owner,{"mint":mint},{"encoding":"jsonParsed"}]}; resp=requests.post(rpc,json=payload,timeout=20); v=resp.json().get("result",{}).get("value",[]); total=sum(float(x["account"]["data"]["parsed"]["info"]["tokenAmount"].get("uiAmountString","0")) for x in v); print(json.dumps({"owner":owner,"balance":total,"token_accounts":len(v)}))'

{"owner": "FMNs7xqezz4bYYioPyfqPzxLLmZyJhjSzbGApMdnrC2Z", "balance": 30.0, "token_accounts": 1}

(base) bubblevan@Bubbles:/mnt/d/MyLab/StablePay/stablepay-openclaw-plugin$ OWNER="FMNs7xqezz4bYYioPyfqPzxLLmZyJhjSzbGApMdnrC2Z" python3 -c 'import os,requests,json; rpc="https://api.devnet.solana.com"; owner=os.environ["OWNER"]; payload={"jsonrpc":"2.0","id":1,"method":"getBalance","params":[owner]}; resp=requests.post(rpc,json=payload,timeout=20).json(); lamports=resp["result"]["value"]; print(json.dumps({"owner":owner,"lamports":lamports,"sol":lamports/1_000_000_000}))'
{"owner": "FMNs7xqezz4bYYioPyfqPzxLLmZyJhjSzbGApMdnrC2Z", "lamports": 4990296280, "sol": 4.99029628}
```
注意下面我们新创了一个卖家的OWS钱包：
```bash
OWNER="2kZGwkLnVdSxjjNueeUQmqBf3tRKMn7y1bbktRZkJWdR" python3 -c 'import os,requests,json; rpc="https://api.devnet.solana.com"; owner=os.environ["OWNER"]; mint="4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU"; payload={"jsonrpc":"2.0","id":1,"method":"getTokenAccountsByOwner","params":[owner,{"mint":mint},{"encoding":"jsonParsed"}]}; resp=requests.post(rpc,json=payload,timeout=20); v=resp.json().get("result",{}).get("value",[]); total=sum(float(x["account"]["data"]["parsed"]["info"]["tokenAmount"].get("uiAmountString","0")) for x in v); print(json.dumps({"owner":owner,"balance":total,"token_accounts":len(v)}))'

{"owner": "2kZGwkLnVdSxjjNueeUQmqBf3tRKMn7y1bbktRZkJWdR", "balance": 0, "token_accounts": 0}
```

得到上述基础内容之后，设置环境变量：
```bash
# export GW="https://ai.wenfu.cn"
export GW="http://127.0.0.1:28080"
export API_KEY="stablepay-dev-key"
export SOLANA_RPC="https://api.devnet.solana.com"
# devnet USDC mint
export USDC_MINT="4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU"
export EMPTY_HASH="e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

# 买家（复用）
export WALLET_AGENT="stablepay-agent"
export WALLET_AGENT_ID="071ccfe1-c6c4-4b4c-bbad-274033f17c61"
export AGENT_SOL="2gL5tHKBp2ZWGX9chgcHFtEqLcwG2GT76CimizqFqXXF"
export AGENT_DID="did:solana:2gL5tHKBp2ZWGX9chgcHFtEqLcwG2GT76CimizqFqXXF"

# 卖家（每次新商品建议新建）
# 如果你是新建卖家钱包，请把下面 4 个值替换成新钱包结果
export NEW_SKILL_WALLET_NAME="real-stablepay-skill"
export NEW_SKILL_WALLET_ID="32b5717f-2a07-4a3b-9632-217dcb77a721"
export NEW_SKILL_SOL="2kZGwkLnVdSxjjNueeUQmqBf3tRKMn7y1bbktRZkJWdR"
export NEW_SKILL_DID="did:solana:${NEW_SKILL_SOL}"

export SKILL_NAME="ManualDemoSkill"
export PRICE="1.00"
export CURRENCY="USDC"
```

第 0 步关键检查（必须通过）：
```bash
# 1) 钱包名必须真实存在；不能用占位符 stablepay-skill-seller-<timestamp>
ows wallet list | rg "Name:\\s+${NEW_SKILL_WALLET_NAME}"

# 2) 该钱包的 Solana 地址必须和 NEW_SKILL_SOL 一致
echo "NEW_SKILL_WALLET_NAME=${NEW_SKILL_WALLET_NAME}"
echo "NEW_SKILL_SOL=${NEW_SKILL_SOL}"
```

如果这里配错，会出现典型现象：
- 终端先报：`error: wallet not found: 'stablepay-skill-seller-<timestamp>'`
- 网关再报：`code=10004 signature verification failed`，detail 常见 `missing did signature fields`

字段解释：
- `WALLET_AGENT`：买家 OWS 钱包名，负责业务签名与交易签名。
- `NEW_SKILL_WALLET_NAME`：本轮“新商品”卖家钱包名，对应新的 `NEW_SKILL_DID`。
- `AGENT_DID` / `NEW_SKILL_DID`：必须与各自钱包公钥一致，否则验签会失败。
- `USDC_MINT`：这里用 devnet 的 `4zMMC...`，不要混成 mainnet 的 `EPjF...`。

## 2）注册买家卖家 DID
```bash
# 注册买家 DID（agent）
curl -s -X POST "${GW}/api/v1/did" \
  -H "Content-Type: application/json" \
  -d "{
    \"user_type\":\"agent\",
    \"metadata\":{
      \"public_key\":\"${AGENT_SOL}\",
      \"wallet_address\":\"${AGENT_SOL}\",
      \"wallet_id\":\"${WALLET_AGENT_ID}\",
      \"source\":\"ows\",
      \"wallet_name\":\"${WALLET_AGENT}\"
    }
  }" | python3 -m json.tool

# 注册卖家 DID（developer，使用 NEW_* 变量）
curl -s -X POST "${GW}/api/v1/did" \
  -H "Content-Type: application/json" \
  -d "{
    \"user_type\":\"developer\",
    \"metadata\":{
      \"public_key\":\"${NEW_SKILL_SOL}\",
      \"wallet_address\":\"${NEW_SKILL_SOL}\",
      \"wallet_id\":\"${NEW_SKILL_WALLET_ID}\",
      \"source\":\"ows\",
      \"wallet_name\":\"${NEW_SKILL_WALLET_NAME}\"
    }
  }" | python3 -m json.tool
```
> 注意：每次重启如果删除了 MySQL volume（如 `down -v`），都需要重新注册 DID。

结果：
```bash
{
    "code": 0,
    "message": "success",
    "data": {
        "created_at": "2026-04-08T14:39:51Z",
        "did": "did:solana:2gL5tHKBp2ZWGX9chgcHFtEqLcwG2GT76CimizqFqXXF",
        "public_key": "2gL5tHKBp2ZWGX9chgcHFtEqLcwG2GT76CimizqFqXXF",
        "wallet_address": "2gL5tHKBp2ZWGX9chgcHFtEqLcwG2GT76CimizqFqXXF",
        "wallet_id": "071ccfe1-c6c4-4b4c-bbad-274033f17c61"
    },
    "request_id": "31d8bfc0-74d5-4230-bfe2-b1ec5968f744",
    "timestamp": "2026-04-08T14:39:51Z"
}

{
    "code": 0,
    "message": "success",
    "data": {
        "created_at": "2026-04-08T14:40:00Z",
        "did": "did:solana:2kZGwkLnVdSxjjNueeUQmqBf3tRKMn7y1bbktRZkJWdR",
        "public_key": "2kZGwkLnVdSxjjNueeUQmqBf3tRKMn7y1bbktRZkJWdR",
        "wallet_address": "2kZGwkLnVdSxjjNueeUQmqBf3tRKMn7y1bbktRZkJWdR",
        "wallet_id": "5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp"
    },
    "request_id": "6c840f2a-ec73-4c8b-a0ce-f2139a9b6392",
    "timestamp": "2026-04-08T14:40:00Z"
}
```

但是我们这里不对劲，不能创建私钥入库。

| 路径 | 路由名 | 下游 RPC | 含义 |
|------|--------|----------|------|
| **`POST /api/v1/did`** | `did.create` | **CreateDID** | 服务端**生成**新密钥对、加密私钥入库，返回新 DID（托管/服务端创建模式）。 |
| **`POST /api/v1/did/register`** | `did.register` | **RegisterDID** | 客户端**自带** `public_key` / `wallet_address`，服务端只做登记（私钥不在服务端；OWS 等场景）。 |

```bash
curl -s -X POST "${GW}/api/v1/did/register" \
  -H "Content-Type: application/json" \
  -d "{
    \"user_type\": \"agent\",
    \"public_key\": \"${AGENT_SOL}\",
    \"wallet_address\": \"${AGENT_SOL}\",
    \"wallet_id\": \"${WALLET_AGENT_ID}\",
    \"wallet_name\": \"${WALLET_AGENT}\",
    \"metadata\": {
      \"source\": \"ows\"
    }
  }"

curl -s -X POST "${GW}/api/v1/did/register" \
  -H "Content-Type: application/json" \
  -d "{
    \"user_type\": \"developer\",
    \"public_key\": \"${NEW_SKILL_SOL}\",
    \"wallet_address\": \"${NEW_SKILL_SOL}\",
    \"wallet_id\": \"${NEW_SKILL_WALLET_ID}\",
    \"wallet_name\": \"${NEW_SKILL_WALLET_NAME}\",
    \"metadata\": {
      \"source\": \"ows\"
    }
  }"
```
## 3）拿支付要求（402 challenge）
```bash
curl -s "${GW}/api/v1/pay/require?skill_did=${NEW_SKILL_DID}&agent_did=${AGENT_DID}&skill_name=ManualDemoSkill2&price=1.00&currency=USDC&message=Pay+to+unlock" | python3 -m json.tool
```
```bash
{
    "code": 402,
    "message": "Payment Required",
    "data": {
        "currency": "USDC",
        "message": "Pay to unlock",
        "payment_endpoint": "/api/v1/pay",
        "price": "1.00",
        "skill_did": "did:solana:2kZGwkLnVdSxjjNueeUQmqBf3tRKMn7y1bbktRZkJWdR",
        "skill_name": "ManualDemoSkill2"
    },
    "request_id": "120e1fe8-56d7-48b4-a71c-ffdd6481e4ab",
    "timestamp": "2026-04-08T14:41:00Z"
}
```
## 4）购买前 verify（应该是 false）
```bash
curl -s "${GW}/api/v1/verify?agent_did=${AGENT_DID}&skill_did=${NEW_SKILL_DID}" \
  -H "X-API-Key: ${API_KEY}" | python3 -m json.tool
```
第一次真实购买前，期望是 purchased: false：
```bash
{
    "code": 0,
    "message": "success",
    "data": {
        "purchased": false
    },
    "request_id": "e86417dc-b991-4ceb-a96e-dfdaa668fad9",
    "timestamp": "2026-04-08T14:41:16Z"
}
```
## 5）辅助函数与编码说明
- 为什么有 `base58`：`X-StablePay-Signature` 和业务签名字段 `signature` 走的是 Base58。
- 为什么有 `base64`：链上交易载荷（`unsigned_tx_base64` / `signed_tx_base64`）是二进制交易字节，通常用 Base64 传输。
- 为什么需要两阶段：服务端没有买家私钥，无法替买家签名；所以必须客户端先本地签名，再把 `signed_tx_base64` 提交到网关 `/api/v1/pay`。

> 你刚才报错 `invalid fee payer address: decode: zero length string` 的根因是旧镜像里 fee payer 为空。  
> 代码已修复为使用 hotwallet 地址做 fee payer，记得重建 `blockchain-adapter`。

基础辅助函数（统一放这里）：
```bash
# message 签名：OWS 十六进制签名 -> Base58（网关和业务签名都用这个）
sign_ows_b58() { ows sign message --wallet "$1" --chain solana --message "$2" --json | jq -r '.signature' | python3 -c 'import sys,base58; h=sys.stdin.read().strip(); h=h[2:] if h.startswith("0x") else h; print(base58.b58encode(bytes.fromhex(h)).decode())'; }

# 编码转换：交易签名时会频繁用到
b64_to_hex() { python3 -c "import base64,sys; print(base64.b64decode(sys.argv[1]).hex())" "$1"; }

hex_to_b64() { python3 -c "import base64,sys; h=sys.argv[1].strip(); h=h[2:] if h.startswith('0x') else h; print(base64.b64encode(bytes.fromhex(h)).decode())" "$1"; }
```
## 6）发起支付
这一步用的是：
- 业务签名：买家 OWS 钱包对支付业务串签名 
- 网关 DID 鉴权签名：买家 OWS 钱包对 canonical 串签名 
必须携带 `signed_tx_base64`（买家已签名的交易载荷）

> 重启清库后的必做前置：
> - 如果 did-service 是新库，未登记 DID 时会报 `401 + code=10004`，did-service 日志会看到 `record not found`。
> - 先确认买家和卖家 DID 已登记，未登记就重新调用 `/api/v1/did`。

先做“与服务端同口径”的余额硬校验（可选但强烈建议）：
```bash
# payment-service 实际是按「买家 DID -> 钱包地址」检查余额，不看卖家余额
# blockchain-adapter 当前实现查的是：该钱包在 USDC 官方 mint 的 ATA 余额（不是 owner 下所有 token account 之和）
python3 - <<'PY' "$AGENT_SOL"
import sys,requests,json
owner=sys.argv[1]
rpc="https://api.devnet.solana.com"
mint="4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU"  # devnet USDC
payload={"jsonrpc":"2.0","id":1,"method":"getTokenAccountsByOwner","params":[owner,{"mint":mint},{"encoding":"jsonParsed"}]}
r=requests.post(rpc,json=payload,timeout=20).json()
arr=r.get("result",{}).get("value",[])
print("token_accounts=",len(arr))
for i,x in enumerate(arr):
    info=x["account"]["data"]["parsed"]["info"]
    print(i, info["mint"], info["owner"], info["tokenAmount"].get("amount","0"), info["tokenAmount"].get("uiAmountString","0"), x.get("pubkey"))
print("sum_minor=",sum(int(x["account"]["data"]["parsed"]["info"]["tokenAmount"].get("amount","0")) for x in arr))
PY
```

### 6.1）生成 `SIGNED_TX_BASE64`（本地签名，fee payer 由平台配置决定）
说明：
- 这一步是“双阶段里客户端签名阶段”，但最终提交仍然只调一次 `/api/v1/pay`。
- 客户端本地构造 unsigned 交易骨架（from=买家，to=卖家），再由买家钱包签名。
- fee payer 地址来自平台配置（本地读取），不是 `/api/v1/pay` 客户端请求参数。
- 请分段执行，不要一次性粘贴多段长命令（会出现命令截断/串行污染）。

关键实现说明（对齐本次修复）：
- Solana 正确签名对象是 `tx.serializeMessage()`（message bytes），不是完整 serialized transaction。
- 因此插件脚本 `scripts/build_signed_tx.mjs` 使用的是 `signSolanaMessageHexWithOwsCli(...)`，本质调用 `ows sign message --encoding hex`。
- 脚本随后用 `tx.addSignature(fromPk, buyerSig)` 把买家签名填回 signer slot，输出“部分签名交易”（buyer 已签，fee payer 待服务端补签）。
- 不建议在业务流程中使用 `ows sign tx` 直签 Solana 整笔交易字节，这与当前“客户端买家签 + 服务端 hotwallet 补签”的模型不匹配。
- `stablepay-openclaw-plugin/dist/ows_sign_tx.js` 是构建产物，禁止手改；如需改签名逻辑，只改 `src/ows_sign_tx.ts` 后执行 `npm run build`。
- 稍微复用一下[插件（真客户端）](https://github.com/Bubblevan/openclaw-plugin-tryon)的工具

```bash
# 先确保金额最小单位已算好（1.00 USDC -> 1000000）
export AMOUNT_MINOR="$(python3 -c "import sys; from decimal import Decimal; print(int(Decimal(sys.argv[1]) * Decimal('1000000')))" "$PRICE")"
# 读取 hotwallet 地址作为 fee payer（只用公钥地址，不需要私钥）
export HOTWALLET_SOL="$(python3 - <<'PY'
import json
print(json.load(open("/mnt/d/MyLab/StablePay/blockchain-adapter/conf/hotwallet.json","r",encoding="utf-8"))["address"])
PY
)"

# 在插件目录安装依赖并构建（首次或依赖变更后执行）
cd /mnt/d/MyLab/StablePay/stablepay-openclaw-plugin
npm install --silent
npm run build --silent

# 复用插件已有签名方法（dist/ows_sign_tx.js）和脚本（scripts/build_signed_tx.mjs）
export SIGNED_TX_JSON="$(
NODE_USE_ENV_PROXY=1 node scripts/build_signed_tx.mjs \
  "$SOLANA_RPC" \
  "$USDC_MINT" \
  "$AGENT_SOL" \
  "$NEW_SKILL_SOL" \
  "$HOTWALLET_SOL" \
  "$AMOUNT_MINOR" \
  "$WALLET_AGENT"
)"
echo "$SIGNED_TX_JSON" | python3 -m json.tool

export SIGNED_TX_BASE64="$(python3 - <<'PY' "$SIGNED_TX_JSON"
import json,sys
obj=json.loads(sys.argv[1])
v=obj.get("signed_tx_base64","")
if not isinstance(v,str) or not v.strip():
    raise SystemExit("build_signed_tx.mjs 输出缺少 signed_tx_base64")
print(v.strip())
PY
)"
echo "SIGNED_TX_BASE64 ready, len=${#SIGNED_TX_BASE64}"
```

说明：
- `(node) [UNDICI-EHPA] Warning: EnvHttpProxyAgent is experimental` 是 Node 的实验特性告警，不影响交易构造和签名结果。
- `bigint: Failed to load bindings, pure JS will be used` 是 bigint 原生绑定缺失回退到 JS 实现，不影响正确性，只可能影响性能。
- 如需消除 `bigint` 回退告警，可在插件目录执行一次：`npm rebuild bigint`。

### 6.2）发起支付（bash，建议按段执行）：
```bash
# ===== 0) 依赖健康检查（避免 did-service 不可用导致 401）=====
docker ps --format 'table {{.Names}}\t{{.Status}}' | egrep 'stablepay-(api-gateway|did-service|payment-service|blockchain-adapter|query-service|verification-service)|NAMES'
docker exec stablepay-api-gateway sh -c "getent hosts stablepay-did-service"

# ===== 固定参数（沿用前面导出的环境变量）=====
# 必须已存在：GW / AGENT_DID / NEW_SKILL_DID / WALLET_AGENT / PRICE / CURRENCY / SIGNED_TX_BASE64
export ORDER_ID="diag-$(date +%s)"
export IDEMPOTENCY_KEY="idem-$(date +%s%3N)-$RANDOM"

# 客户端（买家）预签名交易载荷，不能为空
if [ -z "${SIGNED_TX_BASE64:-}" ]; then
  echo "缺少 SIGNED_TX_BASE64：请先由买家钱包完成交易签名，再调用 /api/v1/pay"
  exit 1
fi

# 两套 timestamp/nonce：不要混用
export GW_TS="$(date -u +%Y-%m-%dT%H:%M:%SZ)"      # 头签名：RFC3339
export GW_NONCE="gw-$(date +%s%3N)-$RANDOM"
export BIZ_TS="$(date +%s)"                        # 业务签名：Unix秒
export BIZ_NONCE="biz-$(date +%s%3N)-$RANDOM"

# ===== 业务签名（payment-service 业务层会校验）=====
# signData(message) = agent_did|skill_did|amount_minor|currency_enum|sha256(signed_tx_base64)
# 实际验签串 = signData(message) + timestamp + nonce
# USDC currency_enum = 1
export SIGNED_TX_HASH="$(python3 - <<'PY' "$SIGNED_TX_BASE64"
import hashlib,sys
print(hashlib.sha256(sys.argv[1].encode()).hexdigest())
PY
)"
export BIZ_MESSAGE="${AGENT_DID}|${NEW_SKILL_DID}|${AMOUNT_MINOR}|1|${SIGNED_TX_HASH}"
export BIZ_SIGN_DATA="${BIZ_MESSAGE}${BIZ_TS}${BIZ_NONCE}"
export BIZ_SIG_B58="$(sign_ows_b58 "$WALLET_AGENT" "$BIZ_SIGN_DATA")"

# ===== 生成 body（紧凑 JSON，确保 hash 稳定）=====
python3 - <<'PY' > /tmp/pay_body.json
import os, json
print(json.dumps({
  "agent_did": os.environ["AGENT_DID"],
  "skill_did": os.environ["NEW_SKILL_DID"],
  "amount": os.environ["PRICE"],
  "currency": os.environ["CURRENCY"],
  "signed_tx_base64": os.environ["SIGNED_TX_BASE64"],
  "signature": os.environ["BIZ_SIG_B58"],
  "order_id": os.environ["ORDER_ID"],
  "timestamp": int(os.environ["BIZ_TS"]),
  "nonce": os.environ["BIZ_NONCE"]
}, separators=(",", ":")))
PY

export BODY_HASH="$(python3 - <<'PY'
import hashlib
print(hashlib.sha256(open("/tmp/pay_body.json","rb").read()).hexdigest())
PY
)"

# ===== 网关签名（api-gateway 中间件校验）=====
# BuildCanonicalV01 = METHOD + "\n" + PATH + "\n" + RAW_QUERY + "\n" + BODY_SHA256
# POST /api/v1/pay 没有 query，所以第三行必须为空行
export CANONICAL="$(printf 'POST\n/api/v1/pay\n\n%s' "$BODY_HASH")"
export GW_SIGN_DATA="${CANONICAL}${GW_TS}${GW_NONCE}"
export GW_SIG_B58="$(sign_ows_b58 "$WALLET_AGENT" "$GW_SIGN_DATA")"

# ===== 调试输出（出401时先看这几行）=====
echo "GW_TS=$GW_TS"
echo "GW_NONCE=$GW_NONCE"
echo "BIZ_TS=$BIZ_TS"
echo "BIZ_NONCE=$BIZ_NONCE"
echo "BODY_HASH=$BODY_HASH"
echo "CANONICAL=$(printf '%q' "$CANONICAL")"
echo "BODY=$(cat /tmp/pay_body.json)"

# ===== 发起支付 =====
curl -i -sS "${GW}/api/v1/pay" \
  -H "Content-Type: application/json" \
  -H "X-Idempotency-Key: ${IDEMPOTENCY_KEY}" \
  -H "X-StablePay-DID: ${AGENT_DID}" \
  -H "X-StablePay-Signature: ${GW_SIG_B58}" \
  -H "X-StablePay-Timestamp: ${GW_TS}" \
  -H "X-StablePay-Nonce: ${GW_NONCE}" \
  --data-binary "@/tmp/pay_body.json"
```

如果这里仍是 401，优先检查这 4 项：
- `X-StablePay-DID` 对应的钱包是否就是 `WALLET_AGENT`（签名钱包和 DID 必须同一把公钥）
- `CANONICAL` 是否严格是四行，且第三行是空行（POST 无 query）
- `X-StablePay-Timestamp` / `X-StablePay-Nonce` 与 `GW_SIGN_DATA` 拼接时用的是同一组值
- 是否重复用了 nonce（重放会失败；重跑时 nonce 必须全新）

如果返回 `402 + code=20001 insufficient balance`，优先检查：
- 你查余额时用的是不是 devnet USDC 官方 mint `4zMMC...`（不是 mainnet `EPjF...`）
- 你的余额是否在“服务端实际查的那个 token account”里（服务端当前按 ATA 口径查，不是 owner 下多账户求和）
- 付款前请确保买家 DID 对应钱包（`AGENT_DID`）本身有 >= `AMOUNT_MINOR`，卖家钱包没钱不影响发起支付

重要说明（避免误判）：
- `/api/v1/balance` 是 query-service 的账本口径（inflow - spent），不是链上实时余额。
- 链上余额请用上面的 Solana RPC 查询命令；两者不一致是设计差异，不是签名错误。
- query-service 当前 SQL 是按 `transaction_records` 聚合，重启容器不会自动重置该表数据。


实测成功返回（2026-04-08）：
```bash
HTTP/1.1 200 OK
Server: hertz
Date: Wed, 08 Apr 2026 17:58:20 GMT
Content-Type: application/json; charset=utf-8
Content-Length: 329
X-Request-Id: 366f6970-a1a3-4aea-b664-63ad52f01b01
X-Trace-Id: 366f6970-a1a3-4aea-b664-63ad52f01b01

{"code":0,"message":"success","data":{"created_at":"2026-04-08T17:58:16Z","status":"PENDING","tx_hash":"3WAn9iQyWfcBAyDjm1XKdkWV6sJXqHAHcpbKfAcmtSCvndN7m5rEGN3AAsLCQqadue9oRDPzHTQpNEPLVWrGhMEx","tx_id":"0b3ba73a-9ee7-408a-a37b-6c61dd0d8c77"},"request_id":"366f6970-a1a3-4aea-b664-63ad52f01b01","timestamp":"2026-04-08T17:58:21Z"}
```
## 7）查询支付状态（GET /api/v1/pay/:tx_id）
- 按 tx_id 查询单笔支付的最终状态
- 这是买家视角查询，所以用买家钱包签名
```bash
# 把这里替换成你刚刚支付成功返回的 tx_id
TX_ID="0b3ba73a-9ee7-408a-a37b-6c61dd0d8c77"

GW_TS="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
GW_NONCE="gw-pay-$(date +%s%3N)-$RANDOM"

# GET /api/v1/pay/{tx_id}
# 第三行是 query string；这里没有 query，所以必须留空行
CANONICAL="$(printf 'GET\n/api/v1/pay/%s\n\n%s' "$TX_ID" "$EMPTY_HASH")"
GW_SIG_B58="$(sign_ows_b58 "$WALLET_AGENT" "${CANONICAL}${GW_TS}${GW_NONCE}")"

curl -sS "${GW}/api/v1/pay/${TX_ID}" \
  -H "X-StablePay-DID: ${AGENT_DID}" \
  -H "X-StablePay-Signature: ${GW_SIG_B58}" \
  -H "X-StablePay-Timestamp: ${GW_TS}" \
  -H "X-StablePay-Nonce: ${GW_NONCE}" | python3 -m json.tool
```
结果为：
```bash
{
    "code": 0,
    "message": "success",
    "data": {
        "agent_did": "did:solana:2gL5tHKBp2ZWGX9chgcHFtEqLcwG2GT76CimizqFqXXF",
        "amount": "1",
        "confirmed_at": "2026-04-08T17:58:24Z",
        "created_at": "2026-04-08T17:58:16Z",
        "currency": "USDC",
        "skill_did": "did:solana:2kZGwkLnVdSxjjNueeUQmqBf3tRKMn7y1bbktRZkJWdR",
        "status": "COMPLETED",
        "tx_hash": "3WAn9iQyWfcBAyDjm1XKdkWV6sJXqHAHcpbKfAcmtSCvndN7m5rEGN3AAsLCQqadue9oRDPzHTQpNEPLVWrGhMEx",
        "tx_id": "0b3ba73a-9ee7-408a-a37b-6c61dd0d8c77"
    },
    "request_id": "a629101b-6d78-4425-aec2-3cfe3731fcf8",
    "timestamp": "2026-04-08T18:21:24Z"
}
```
## 8）查询 transactions（买家视角，GET /api/v1/transactions）
- 查询买家自己的交易列表
- 这是买家视角查询，所以仍然用买家钱包签名
> 当前实现里用的是 agent_did 查询参数。虽然文档里对交易记录查询写得更泛化，但你这套实际跑通的命令就按当前实现保留 agent_did。
```bash
GW_TS="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
GW_NONCE="gw-txs-$(date +%s%3N)-$RANDOM"
QUERY="agent_did=${AGENT_DID}&page=1&page_size=10"

CANONICAL="$(printf 'GET\n/api/v1/transactions\n%s\n%s' "$QUERY" "$EMPTY_HASH")"
GW_SIG_B58="$(sign_ows_b58 "$WALLET_AGENT" "${CANONICAL}${GW_TS}${GW_NONCE}")"

curl -sS "${GW}/api/v1/transactions?${QUERY}" \
  -H "X-StablePay-DID: ${AGENT_DID}" \
  -H "X-StablePay-Signature: ${GW_SIG_B58}" \
  -H "X-StablePay-Timestamp: ${GW_TS}" \
  -H "X-StablePay-Nonce: ${GW_NONCE}" | python3 -m json.tool
```
结果为
```bash
{
    "code": 0,
    "message": "success",
    "data": {
        "items": [
            {
                "amount_minor": 1000000,
                "created_at": "2026-04-08T17:58:24Z",
                "currency": 1,
                "skill_did": "did:solana:2kZGwkLnVdSxjjNueeUQmqBf3tRKMn7y1bbktRZkJWdR",
                "tx_id": "0b3ba73a-9ee7-408a-a37b-6c61dd0d8c77",
                "type": 1
            }
        ]
    },
    "request_id": "2b2da21a-b3f2-4c2f-86a2-9b85e4917863",
    "timestamp": "2026-04-08T18:22:53Z"
}
```
> 这里items为空是不正常现象
## 9) 查 pay history（买家视角）
- 查询 Payment Service 视角下的支付历史
- 也是买家视角，继续用买家钱包签名
```bash
GW_TS="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
GW_NONCE="gw-history-$(date +%s%3N)-$RANDOM"
QUERY="agent_did=${AGENT_DID}&page=1&page_size=10"

CANONICAL="$(printf 'GET\n/api/v1/pay/history\n%s\n%s' "$QUERY" "$EMPTY_HASH")"
GW_SIGN_DATA="${CANONICAL}${GW_TS}${GW_NONCE}"
GW_SIG_B58="$(sign_ows_b58 "$WALLET_AGENT" "$GW_SIGN_DATA")"

curl -s "${GW}/api/v1/pay/history?${QUERY}" \
  -H "X-StablePay-DID: ${AGENT_DID}" \
  -H "X-StablePay-Signature: ${GW_SIG_B58}" \
  -H "X-StablePay-Timestamp: ${GW_TS}" \
  -H "X-StablePay-Nonce: ${GW_NONCE}" | python3 -m json.tool
```
结果为
```bash
{
    "code": 0,
    "message": "success",
    "data": {
        "items": [
            {
                "amount": "1",
                "created_at": "2026-04-08T17:58:16Z",
                "currency": "USDC",
                "skill_did": "did:solana:2kZGwkLnVdSxjjNueeUQmqBf3tRKMn7y1bbktRZkJWdR",
                "status": "COMPLETED",
                "tx_id": "0b3ba73a-9ee7-408a-a37b-6c61dd0d8c77"
            }
        ],
        "page": 1,
        "page_size": 20,
        "total": 1
    },
    "request_id": "5ea26266-2291-4494-ae44-07fbf98eddc7",
    "timestamp": "2026-04-08T18:23:21Z"
}
```
> 这里items为空是不正常现象

## 10) 查 verify / proof（开发者后端视角，走 API Key）
- 这是开发者后端防篡改校验的标准接口
- 不需要 DID 签名，直接走 X-API-Key
- verify 看是否已购；proof 看完整购买证明
- 这条链是支付成功后由支付事件更新购买记录，所以如果刚支付完马上查到 false，更像是异步更新还没完成；正常最终应为 true。
```bash
curl -s "${GW}/api/v1/verify?agent_did=${AGENT_DID}&skill_did=${NEW_SKILL_DID}" \
  -H "X-API-Key: ${API_KEY}" | python3 -m json.tool

{
    "code": 0,
    "message": "success",
    "data": {
        "amount_minor": 1000000,
        "purchase_time": "2026-04-08T17:58:24Z",
        "purchased": true,
        "tx_id": "0b3ba73a-9ee7-408a-a37b-6c61dd0d8c77"
    },
    "request_id": "4c498cd7-3211-47d8-9107-0198c739eb20",
    "timestamp": "2026-04-08T17:58:32Z"
}
```
```bash
curl -s "${GW}/api/v1/verify/proof?agent_did=${AGENT_DID}&skill_did=${NEW_SKILL_DID}" \
  -H "X-API-Key: ${API_KEY}" | python3 -m json.tool

{
    "code": 0,
    "message": "success",
    "data": {
        "amount_minor": 1000000,
        "proof_version": "v1",
        "purchase_time": "2026-04-08T17:58:24Z",
        "purchased": true,
        "tx_hash": "3WAn9iQyWfcBAyDjm1XKdkWV6sJXqHAHcpbKfAcmtSCvndN7m5rEGN3AAsLCQqadue9oRDPzHTQpNEPLVWrGhMEx",
        "tx_id": "0b3ba73a-9ee7-408a-a37b-6c61dd0d8c77"
    },
    "request_id": "4d15455b-d3ed-41a6-82a9-baa416e99158",
    "timestamp": "2026-04-08T17:58:36Z"
}
```
出现下面的情况是不正常现象：
```bash
{
    "code": 0,
    "message": "success",
    "data": {
        "purchased": false
    },
    "request_id": "974f9dd9-13ed-4db8-b15b-742be894bf41",
    "timestamp": "2026-04-08T13:01:48Z"
}
```

## 11）查询账本余额（买家 DID 视角，GET /api/v1/balance，注意参数必须是 `agent_did`）
- 这一步查的是 StablePay Query Service 的业务余额视图，不是第 1 步那种链上直查余额。查询链路会经过 Query Service，并由它去取链上/缓存/统计数据。
- 参数必须是 agent_did
- 这是买家视角，所以用买家钱包签名
```bash
GW_TS="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
GW_NONCE="gw-balance-$(date +%s%3N)-$RANDOM"
QUERY="agent_did=${AGENT_DID}"

CANONICAL="$(printf 'GET\n/api/v1/balance\n%s\n%s' "$QUERY" "$EMPTY_HASH")"
GW_SIG_B58="$(sign_ows_b58 "$WALLET_AGENT" "${CANONICAL}${GW_TS}${GW_NONCE}")"

curl -sS "${GW}/api/v1/balance?${QUERY}" \
  -H "X-StablePay-DID: ${AGENT_DID}" \
  -H "X-StablePay-Signature: ${GW_SIG_B58}" \
  -H "X-StablePay-Timestamp: ${GW_TS}" \
  -H "X-StablePay-Nonce: ${GW_NONCE}" | python3 -m json.tool
```
结果为：
```bash
{
    "code": 0,
    "message": "success",
    "data": {
        "balance_minor": 39000000,
        "currency": 1,
        "monthly_limit_minor": 50000000000,
        "monthly_spent_minor": 0
    },
    "request_id": "cae1fe59-0f64-46cf-9acc-29e5267f142c",
    "timestamp": "2026-04-08T19:07:24Z"
}
```

## 12）查 sales（卖家 / skill 视角）
- 查询某个 skill 的销售记录
- 这是卖家 / 开发者视角
- 必须用 **卖家钱包** 去签，不是买家钱包
```bash
GW_TS="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
GW_NONCE="gw-sales-$(date +%s%3N)-$RANDOM"
QUERY="skill_did=${NEW_SKILL_DID}&limit=10"

CANONICAL="$(printf 'GET\n/api/v1/sales\n%s\n%s' "$QUERY" "$EMPTY_HASH")"
GW_SIGN_DATA="${CANONICAL}${GW_TS}${GW_NONCE}"
GW_SIG_B58="$(sign_ows_b58 "$NEW_SKILL_WALLET_NAME" "$GW_SIGN_DATA")"

curl -s "${GW}/api/v1/sales?${QUERY}" \
  -H "X-StablePay-DID: ${NEW_SKILL_DID}" \
  -H "X-StablePay-Signature: ${GW_SIG_B58}" \
  -H "X-StablePay-Timestamp: ${GW_TS}" \
  -H "X-StablePay-Nonce: ${GW_NONCE}" | python3 -m json.tool
```
结果为：
```bash
{
    "code": 0,
    "message": "success",
    "data": {
        "base": {
            "code": 0,
            "message": "success"
        },
        "items": [
            {
                "agent_did": "did:solana:2gL5tHKBp2ZWGX9chgcHFtEqLcwG2GT76CimizqFqXXF",
                "amount_minor": 1000000,
                "created_at": "2026-04-08T19:04:48Z",
                "currency": "USDC",
                "skill_did": "did:solana:2kZGwkLnVdSxjjNueeUQmqBf3tRKMn7y1bbktRZkJWdR",
                "source_tx_id": "2e20d2a7-1fa3-4ce1-9704-44b6143e1eec",
                "tx_id": "2e20d2a7-1fa3-4ce1-9704-44b6143e1eec"
            }
        ],
        "limit": 10,
        "offset": 0,
        "skill_did": "did:solana:2kZGwkLnVdSxjjNueeUQmqBf3tRKMn7y1bbktRZkJWdR",
        "total": 1
    },
    "request_id": "dc939423-6954-4b99-9502-56c1ac11510d",
    "timestamp": "2026-04-08T19:07:37Z"
}
```
## 13) 查 revenue（卖家 / skill 视角）
```bash
GW_TS="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
GW_NONCE="gw-revenue-$(date +%s%3N)-$RANDOM"
QUERY="skill_did=${NEW_SKILL_DID}"
CANONICAL="$(printf 'GET\n/api/v1/revenue\n%s\n%s' "$QUERY" "$EMPTY_HASH")"
GW_SIGN_DATA="${CANONICAL}${GW_TS}${GW_NONCE}"
GW_SIG_B58="$(sign_ows_b58 "$NEW_SKILL_WALLET_NAME" "$GW_SIGN_DATA")"

curl -s "${GW}/api/v1/revenue?${QUERY}" \
  -H "X-StablePay-DID: ${NEW_SKILL_DID}" \
  -H "X-StablePay-Signature: ${GW_SIG_B58}" \
  -H "X-StablePay-Timestamp: ${GW_TS}" \
  -H "X-StablePay-Nonce: ${GW_NONCE}" | python3 -m json.tool
```
结果为：
```bash
{
    "code": 0,
    "message": "success",
    "data": {
        "currency": 1,
        "sales_trend": [
            {
                "date": "2026-04-08",
                "amount_minor": 1000000
            }
        ],
        "total_revenue_minor": 1000000,
        "total_sales": 1
    },
    "request_id": "11ac1451-9252-4aac-8ecb-e83be7acae7d",
    "timestamp": "2026-04-08T19:07:49Z"
}
```
## 14) 买卖双方支付后，再直查一次链上 USDC 余额
包括买家、卖家还有平台热钱包：
```bash
(base) bubblevan@Bubbles:/mnt/d/MyLab/StablePay/stablepay-openclaw-plugin$ OWNER="2gL5tHKBp2ZWGX9chgcHFtEqLcwG2GT76CimizqFqXXF" python3 -c 'import os,requests,json; rpc="https://api.devnet.solana.com"; owner=os.environ["OWNER"]; mint="4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU"; payload={"jsonrpc":"2.0","id":1,"method":"getTokenAccountsByOwner","params":[owner,{"mint":mint},{"encoding":"jsonParsed"}]}; resp=requests.post(rpc,json=payload,timeout=20); v=resp.json().get("result",{}).get("value",[]); total=sum(float(x["account"]["data"]["parsed"]["info"]["tokenAmount"].get("uiAmountString","0")) for x in v); print(json.dumps({"owner":owner,"balance":total,"token_accounts":len(v)}))'
{"owner": "2gL5tHKBp2ZWGX9chgcHFtEqLcwG2GT76CimizqFqXXF", "balance": 39.0, "token_accounts": 1}

(base) bubblevan@Bubbles:/mnt/d/MyLab/StablePay/stablepay-openclaw-plugin$ OWNER="FMNs7xqezz4bYYioPyfqPzxLLmZyJhjSzbGApMdnrC2Z" python3 -c 'import os,requests,json; rpc="https://api.devnet.solana.com"; owner=os.environ["OWNER"]; payload={"jsonrpc":"2.0","id":1,"method":"getBalance","params":[owner]}; resp=requests.post(rpc,json=payload,timeout=20).json(); lamports=resp["result"]["value"]; print(json.dumps({"owner":owner,"lamports":lamports,"sol":lamports/1_000_000_000}))'
{"owner": "FMNs7xqezz4bYYioPyfqPzxLLmZyJhjSzbGApMdnrC2Z", "lamports": 4990286280, "sol": 4.99028628}

(base) bubblevan@Bubbles:/mnt/d/MyLab/StablePay/stablepay-openclaw-plugin$ OWNER="2kZGwkLnVdSxjjNueeUQmqBf3tRKMn7y1bbktRZkJWdR" python3 -c 'import os,requests,json; rpc="https://api.devnet.solana.com"; owner=os.environ["OWNER"]; mint="4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU"; payload={"jsonrpc":"2.0","id":1,"method":"getTokenAccountsByOwner","params":[owner,{"mint":mint},{"encoding":"jsonParsed"}]}; resp=requests.post(rpc,json=payload,timeout=20); v=resp.json().get("result",{}).get("value",[]); total=sum(float(x["account"]["data"]["parsed"]["info"]["tokenAmount"].get("uiAmountString","0")) for x in v); print(json.dumps({"owner":owner,"balance":total,"token_accounts":len(v)}))'
{"owner": "2kZGwkLnVdSxjjNueeUQmqBf3tRKMn7y1bbktRZkJWdR", "balance": 1.0, "token_accounts": 1}
```
- USDC 的 1 块钱是从买家转到卖家了
- 顺手创建了卖家的 ATA（Associated Token Account）
- hotwallet 真正承担的是 SOL 手续费，不是 USDC
