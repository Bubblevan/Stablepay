import { createServer } from "node:http";
import { URL } from "node:url";
import crypto from "node:crypto";
import { config } from "dotenv";

// 从 .env 文件加载环境变量
config();

const PORT = Number(process.env.PORT || 8787);
const GATEWAY_BASE_URL = process.env.GATEWAY_BASE_URL || "https://ai.wenfu.cn";
const STABLEPAY_API_KEY = process.env.STABLEPAY_API_KEY || "stablepay-dev-key";

const SKILL_DID = process.env.SKILL_DID;
const SKILL_NAME = process.env.SKILL_NAME;
const PRICE = process.env.PRICE;
const CURRENCY = process.env.CURRENCY || "USDC";
const MESSAGE = process.env.MESSAGE;

// 该 secret 只在商户后端内部使用，不会暴露给客户端，仅用于生成访问 proof
const MERCHANT_PROOF_SECRET =
  process.env.MERCHANT_PROOF_SECRET || "replaces-this-with-a-long-random-secret";

// 是否暴露调试接口（开发者模式）
const ENABLE_DEBUG_ROUTES = process.env.ENABLE_DEBUG_ROUTES === "1";

// 可选：内部服务的调用地址
const INTERNAL_BASE_URL = process.env.INTERNAL_BASE_URL || "http://127.0.0.1:8184";

// Facilitator 配置（x402 标准）
const FACILITATOR_URL = process.env.FACILITATOR_URL || "https://ai.wenfu.cn";

// USDC 合约地址 (Solana Mainnet)
const USDC_MINT = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v";

// 报告商店配置
const REPORT_STORE_URL = process.env.REPORT_STORE_URL || "http://127.0.0.1:8788";

// 内置报告列表（购买 skill 后解锁）
const UNLOCKED_REPORTS = [
  {
    id: "ai-agent-job-2025",
    title: "AI Agent 岗位分析报告 2025",
    description: "深入分析 2025 年 AI Agent 领域的岗位需求、技能要求、薪资水平和发展趋势",
    price: "2.00",
    currency: "USDC",
    author: "StablePay Research",
    tags: ["AI", "Agent", "求职", "行业分析"]
  },
  {
    id: "industry-briefing-q1",
    title: "2025 Q1 行业研究简报",
    description: "涵盖 AI、区块链、Web3 领域的最新趋势和投资机会",
    price: "1.50",
    currency: "USDC",
    author: "StablePay Research",
    tags: ["行业研究", "AI", "区块链", "Web3"]
  },
  {
    id: "resume-optimization-guide",
    title: "简历优化建议报告",
    description: "针对技术岗位的简历优化建议，包含模板和案例分析",
    price: "1.00",
    currency: "USDC",
    author: "StablePay Research",
    tags: ["求职", "简历", "技术岗位"]
  },
  {
    id: "vitality-research",
    title: "生命力研究：为什么有些人看起来生命力很强",
    description: "基于萨特《恶心》的存在主义解读，探讨生命力的本质与来源",
    price: "1.50",
    currency: "USDC",
    author: "StablePay Research",
    tags: ["哲学", "心理学", "存在主义", "个人成长"]
  }
];

// 从 SKILL_DID 提取收款地址
function getSellerAddress() {
  // did:solana:4p8F5YAJM8fdrNyvWfb3p6XHx8rboFVV3xn279VXo2j7 -> 4p8F5YAJM8fdrNyvWfb3p6XHx8rboFVV3xn279VXo2j7
  if (SKILL_DID && SKILL_DID.startsWith("did:solana:")) {
    return SKILL_DID.replace("did:solana:", "");
  }
  return SKILL_DID || "";
}

// USDC 金额转换 (6位精度)
function usdcToMinorUnits(amount) {
  return Math.round(parseFloat(amount) * 1_000_000).toString();
}

async function readJson(response) {
  const text = await response.text();
  if (!text) return {};
  try {
    return JSON.parse(text);
  } catch {
    return { raw: text };
  }
}

function writeJson(res, status, payload, headers = {}) {
  res.statusCode = status;
  res.setHeader("Content-Type", "application/json; charset=utf-8");
  Object.entries(headers).forEach(([key, value]) => {
    res.setHeader(key, value);
  });
  res.end(JSON.stringify(payload, null, 2));
}

function nowIso() {
  return new Date().toISOString();
}

// 日志工具
function logRequest(method, path, params, status, duration, extra = {}) {
  const timestamp = new Date().toLocaleTimeString();
  const paramsStr = Object.keys(params).length > 0
    ? '?' + Object.entries(params).map(([k, v]) => `${k}=${v}`).join('&')
    : '';

  console.log(`[${timestamp}] ${method} ${path}${paramsStr} -> ${status} (${duration}ms)`);

  if (extra.agentDid) {
    console.log(`  Agent: ${extra.agentDid}`);
  }
  if (extra.purchased !== undefined) {
    console.log(`  Purchased: ${extra.purchased}`);
  }
  if (extra.paymentSignature) {
    console.log(`  Payment-Signature: detected`);
  }
  if (extra.error) {
    console.log(`  Error: ${extra.error}`);
  }
}

// 构建 x402 标准的 Payment-Required 响应头
function buildX402PaymentRequired() {
  const sellerAddress = getSellerAddress();
  const paymentDetails = {
    x402Version: 1,
    accepts: [
      {
        scheme: "exact",
        network: "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp",
        maxAmountRequired: usdcToMinorUnits(PRICE),
        payTo: sellerAddress,
        asset: USDC_MINT,
        description: MESSAGE || `购买 ${SKILL_NAME}`,
        resource: "/execute",
        maxTimeoutSeconds: 300,
        extra: {
          facilitatorUrl: FACILITATOR_URL,
          currency: CURRENCY,
          skillDid: SKILL_DID,
          skillName: SKILL_NAME,
        }
      }
    ],
    error: "Payment Required"
  };

  return Buffer.from(JSON.stringify(paymentDetails)).toString("base64");
}

// 构建 x402 标准的 Payment-Response 响应头
function buildX402PaymentResponse(txId, txHash) {
  const response = {
    x402Version: 1,
    txId,
    txHash,
    settledAt: nowIso(),
  };
  return Buffer.from(JSON.stringify(response)).toString("base64");
}

function buildProof({ agentDid, skillDid, requestText }) {
  const issuedAt = nowIso();
  const nonce = crypto.randomBytes(12).toString("hex");
  const canonical = [
    "showmethemoney-pro",
    agentDid,
    skillDid,
    requestText || "",
    issuedAt,
    nonce,
  ].join("|");

  const signature = crypto
    .createHmac("sha256", MERCHANT_PROOF_SECRET)
    .update(canonical, "utf8")
    .digest("hex");

  return {
    proof_id: `smtm_${nonce}`,
    issued_at: issuedAt,
    canonical,
    signature,
    display_token: `SHOW-ME-THE-MONEY::${signature.slice(0, 24).toUpperCase()}`,
  };
}

async function fetchVerify(agentDid) {
  const url = new URL("/api/v1/verify", GATEWAY_BASE_URL);
  url.searchParams.set("agent_did", agentDid);
  url.searchParams.set("skill_did", SKILL_DID);

  const response = await fetch(url, {
    headers: {
      "X-API-Key": STABLEPAY_API_KEY,
    },
  });

  return { status: response.status, body: await readJson(response) };
}

async function fetchPayRequirement(agentDid) {
  const url = new URL("/api/v1/pay/require", GATEWAY_BASE_URL);
  url.searchParams.set("skill_did", SKILL_DID);
  url.searchParams.set("agent_did", agentDid);
  url.searchParams.set("skill_name", SKILL_NAME);
  url.searchParams.set("price", PRICE);
  url.searchParams.set("currency", CURRENCY);
  url.searchParams.set("message", MESSAGE);

  const response = await fetch(url);
  return { status: response.status, body: await readJson(response) };
}

// x402 标准：解析 Payment-Signature header
function parsePaymentSignature(headerValue) {
  if (!headerValue) return null;
  try {
    const decoded = Buffer.from(headerValue, "base64").toString("utf8");
    return JSON.parse(decoded);
  } catch {
    return null;
  }
}

// 提交支付到 Facilitator 进行结算
async function settlePaymentViaFacilitator(paymentData) {
  const url = new URL("/api/v1/pay", FACILITATOR_URL);

  const response = await fetch(url, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "X-API-Key": STABLEPAY_API_KEY,
    },
    body: JSON.stringify(paymentData),
  });

  return { status: response.status, body: await readJson(response) };
}

// -------- 可选：内部接口调用（调试用） --------

async function fetchRevenue(skillDid) {
  const url = new URL("/internal/revenue", INTERNAL_BASE_URL);
  url.searchParams.set("skill_did", skillDid || SKILL_DID);
  const response = await fetch(url);
  return { status: response.status, body: await readJson(response) };
}

async function fetchSales(skillDid) {
  const url = new URL("/internal/sales", INTERNAL_BASE_URL);
  url.searchParams.set("skill_did", skillDid || SKILL_DID);
  const response = await fetch(url);
  return { status: response.status, body: await readJson(response) };
}

async function fetchAgentTransactions(agentDid) {
  const url = new URL("/internal/transactions", INTERNAL_BASE_URL);
  url.searchParams.set("did", agentDid);
  url.searchParams.set("type", "1");
  const response = await fetch(url);
  return { status: response.status, body: await readJson(response) };
}

async function fetchAgentBalance(agentDid) {
  const url = new URL("/internal/balance", INTERNAL_BASE_URL);
  url.searchParams.set("agent_did", agentDid);
  const response = await fetch(url);
  return { status: response.status, body: await readJson(response) };
}

// -------- x402 核心业务接口 --------

async function handleExecute(req, res, url, headers, startTime) {
  const agentDid = url.searchParams.get("agent_did");
  const requestText =
    url.searchParams.get("q") ||
    url.searchParams.get("prompt") ||
    "default-premium-request";

  const params = { q: requestText };
  if (agentDid) params.agent_did = agentDid.substring(0, 20) + "...";

  if (!agentDid) {
    writeJson(res, 400, {
      code: "missing_param",
      message: "agent_did is required",
      expected: "GET /execute?agent_did=<did>&q=<optional_text>",
    });
    logRequest("GET", "/execute", params, 400, Date.now() - startTime, {
      error: "missing agent_did"
    });
    return;
  }

  // x402 标准：检查 Payment-Signature header（支付凭证）
  const paymentSignature = headers["payment-signature"] || headers["X-Payment-Signature"];
  const paymentData = parsePaymentSignature(paymentSignature);

  // 如果提供了 Payment-Signature，先尝试结算支付
  if (paymentData) {
    console.log("[x402] Payment-Signature detected, attempting settlement...");
    const settleResult = await settlePaymentViaFacilitator({
      agent_did: agentDid,
      skill_did: SKILL_DID,
      price: usdcToMinorUnits(PRICE),
      currency: CURRENCY,
      signature: paymentData.signature,
      sign_nonce: paymentData.nonce,
      sign_timestamp: paymentData.timestamp,
    });

    if (settleResult.status >= 200 && settleResult.status < 300) {
      console.log("[x402] Payment settled successfully:", settleResult.body);
    } else {
      console.log("[x402] Payment settlement failed:", settleResult.body);
    }
  }

  const verify = await fetchVerify(agentDid);

  if (verify.status >= 400) {
    writeJson(res, 502, {
      code: "verify_failed",
      message: "failed to verify purchase state with facilitator",
      facilitator_verify: verify,
    });
    logRequest("GET", "/execute", params, 502, Date.now() - startTime, {
      agentDid: agentDid.substring(0, 20) + "...",
      error: `verify failed: ${verify.status}`
    });
    return;
  }

  const purchased = Boolean(verify.body?.data?.purchased || verify.body?.purchased);

  if (!purchased) {
    // x402 标准：返回 402 并设置 Payment-Required header
    const paymentRequiredHeader = buildX402PaymentRequired();
    const sellerAddress = getSellerAddress();

    writeJson(res, 402, {
      x402Version: 1,
      accepts: [
        {
          scheme: "exact",
          network: "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp",
          maxAmountRequired: usdcToMinorUnits(PRICE),
          payTo: sellerAddress,
          asset: USDC_MINT,
          description: MESSAGE || `购买 ${SKILL_NAME}`,
          resource: "/execute",
          maxTimeoutSeconds: 300,
          extra: {
            facilitatorUrl: FACILITATOR_URL,
            currency: CURRENCY,
            skillDid: SKILL_DID,
            skillName: SKILL_NAME,
          }
        }
      ],
      error: "Payment Required"
    }, {
      "Payment-Required": paymentRequiredHeader,
      "Accept-Payment": "x402, stablepay-v1",
    });
    logRequest("GET", "/execute", params, 402, Date.now() - startTime, {
      agentDid: agentDid.substring(0, 20) + "...",
      purchased: false,
      paymentSignature: !!paymentData
    });
    return;
  }

  const proof = buildProof({
    agentDid,
    skillDid: SKILL_DID,
    requestText,
  });

  // x402 标准：返回 Payment-Response header
  const txId = verify.body?.data?.tx_id || verify.body?.tx_id;
  const txHash = verify.body?.data?.tx_hash || verify.body?.tx_hash;
  const paymentResponseHeader = txId ? buildX402PaymentResponse(txId, txHash) : null;

  const responseHeaders = {};
  if (paymentResponseHeader) {
    responseHeaders["Payment-Response"] = paymentResponseHeader;
  }

  // 构建解锁报告商店的信息
  const unlockedStore = {
    membership_active: true,
    message: "恭喜！您已解锁 Premium Research Store（付费报告商店）。现在可以单独购买研究报告。",
    report_store: {
      base_url: REPORT_STORE_URL,
      browse_endpoint: "/reports",
      purchase_endpoint: "/execute",
      available_reports: UNLOCKED_REPORTS.map(r => ({
        id: r.id,
        title: r.title,
        description: r.description,
        price: r.price,
        currency: r.currency,
        author: r.author,
        tags: r.tags
      }))
    },
    next_steps: [
      "1. 查看可购买的报告列表",
      "2. 选择感兴趣的研究报告",
      "3. 单独付费解锁完整内容"
    ]
  };

  writeJson(res, 200, {
    ok: true,
    product: "showmethemoney-pro",
    x402Version: 1,
    protected_result: {
      kind: "merchant-generated-proof",
      message: "Show me the money: premium access granted",
      request_text: requestText,
      proof,
    },
    access: {
      agent_did: agentDid,
      skill_did: SKILL_DID,
      verified_by_backend: true,
      verified_at: nowIso(),
    },
    // 关键：解锁报告商店的信息
    unlocked_store: unlockedStore,
    verify_snapshot: verify.body,
  }, responseHeaders);

  logRequest("GET", "/execute", params, 200, Date.now() - startTime, {
    agentDid: agentDid.substring(0, 20) + "...",
    purchased: true,
    proofId: proof.proof_id,
    unlockedReports: UNLOCKED_REPORTS.length
  });
}

const server = createServer(async (req, res) => {
  const startTime = Date.now();
  try {
    const url = new URL(
      req.url || "/",
      `http://${req.headers.host || `127.0.0.1:${PORT}`}`,
    );

    // 收集 headers
    const headers = {};
    for (const [key, value] of Object.entries(req.headers)) {
      headers[key.toLowerCase()] = value;
    }

    if (req.method === "GET" && url.pathname === "/healthz") {
      writeJson(res, 200, {
        ok: true,
        service: "showmethemoney-pro-backend",
        x402Version: 1,
        skill_name: SKILL_NAME,
        skill_did: SKILL_DID,
        facilitator_url: FACILITATOR_URL,
        gateway_base_url: GATEWAY_BASE_URL,
      });
      logRequest("GET", "/healthz", {}, 200, Date.now() - startTime);
      return;
    }

    // x402 标准核心业务接口
    if (req.method === "GET" && url.pathname === "/execute") {
      await handleExecute(req, res, url, headers, startTime);
      return;
    }

    // 以下为调试专用内部接口，仅当启用调试模式时可用
    if (ENABLE_DEBUG_ROUTES && req.method === "GET" && url.pathname === "/developer/revenue") {
      const result = await fetchRevenue(url.searchParams.get("skill_did"));
      writeJson(res, result.status, result.body);
      return;
    }

    if (ENABLE_DEBUG_ROUTES && req.method === "GET" && url.pathname === "/developer/sales") {
      const result = await fetchSales(url.searchParams.get("skill_did"));
      writeJson(res, result.status, result.body);
      return;
    }

    if (ENABLE_DEBUG_ROUTES && req.method === "GET" && url.pathname === "/agent/balance") {
      const agentDid = url.searchParams.get("agent_did");
      if (!agentDid) {
        writeJson(res, 400, { code: "missing_param", message: "agent_did is required" });
        return;
      }
      const result = await fetchAgentBalance(agentDid);
      writeJson(res, result.status, result.body);
      return;
    }

    if (ENABLE_DEBUG_ROUTES && req.method === "GET" && url.pathname === "/agent/transactions") {
      const agentDid = url.searchParams.get("agent_did");
      if (!agentDid) {
        writeJson(res, 400, { code: "missing_param", message: "agent_did is required" });
        return;
      }
      const result = await fetchAgentTransactions(agentDid);
      writeJson(res, result.status, result.body);
      return;
    }

    writeJson(res, 404, {
      code: "not_found",
      message: "route not found",
    });
  } catch (error) {
    writeJson(res, 500, {
      code: "merchant_backend_error",
      message: error instanceof Error ? error.message : String(error),
    });
  }
});

server.listen(PORT, () => {
  console.log(`showmethemoney pro backend (x402 compliant) listening on http://127.0.0.1:${PORT}`);
  console.log(`facilitator=${FACILITATOR_URL}`);
  console.log(`skill_did=${SKILL_DID}`);
  console.log(`skill_name=${SKILL_NAME}`);
  console.log(`price=${PRICE} ${CURRENCY}`);
});
