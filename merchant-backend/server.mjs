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
// 在实际部署时请务必替换为一个随机长字符串
const MERCHANT_PROOF_SECRET =
  process.env.MERCHANT_PROOF_SECRET || "replaces-this-with-a-long-random-secret";

// 是否暴露调试接口（开发者模式）
const ENABLE_DEBUG_ROUTES = process.env.ENABLE_DEBUG_ROUTES === "1";

// 可选：内部服务的调用地址
const INTERNAL_BASE_URL = process.env.INTERNAL_BASE_URL || "http://127.0.0.1:8184";

async function readJson(response) {
  const text = await response.text();
  if (!text) return {};
  try {
    return JSON.parse(text);
  } catch {
    return { raw: text };
  }
}

function writeJson(res, status, payload) {
  res.statusCode = status;
  res.setHeader("Content-Type", "application/json; charset=utf-8");
  res.end(JSON.stringify(payload, null, 2));
}

function nowIso() {
  return new Date().toISOString();
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

// -------- 核心业务接口：执行与验证购买 --------

async function handleExecute(req, res, url) {
  // 校验技能 / 验证购买状态 / 返回访问证明
  const agentDid = url.searchParams.get("agent_did");
  const requestText =
    url.searchParams.get("q") ||
    url.searchParams.get("prompt") ||
    "default-premium-request";

  if (!agentDid) {
    writeJson(res, 400, {
      code: "missing_param",
      message: "agent_did is required",
      expected: "GET /execute?agent_did=<did>&q=<optional_text>",
    });
    return;
  }

  const verify = await fetchVerify(agentDid);

  if (verify.status >= 400) {
    writeJson(res, 502, {
      code: "verify_failed",
      message: "failed to verify purchase state with stablepay gateway",
      gateway_verify: verify,
    });
    return;
  }

  const purchased = Boolean(verify.body?.data?.purchased || verify.body?.purchased);

  if (!purchased) {
    const requirement = await fetchPayRequirement(agentDid);
    const pr = requirement.body?.data || requirement.body || {};

    writeJson(res, 402, {
      code: 402,
      message: "Payment Required",
      agent_did: agentDid,
      skill_did: pr.skill_did || SKILL_DID,
      skill_name: pr.skill_name || SKILL_NAME,
      price: pr.price || PRICE,
      currency: pr.currency || CURRENCY,
      payment_endpoint: pr.payment_endpoint || "/api/v1/pay",
      merchant_backend: {
        endpoint: "/execute",
        request_text: requestText,
      },
      payment_requirement: requirement.body,
    });
    return;
  }

  const proof = buildProof({
    agentDid,
    skillDid: SKILL_DID,
    requestText,
  });

  writeJson(res, 200, {
    ok: true,
    product: "showmethemoney-pro",
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
    verify_snapshot: verify.body,
  });
}

const server = createServer(async (req, res) => {
  try {
    const url = new URL(
      req.url || "/",
      `http://${req.headers.host || `127.0.0.1:${PORT}`}`,
    );

    if (req.method === "GET" && url.pathname === "/healthz") {
      writeJson(res, 200, {
        ok: true,
        service: "showmethemoney-pro-backend",
        skill_name: SKILL_NAME,
        skill_did: SKILL_DID,
        price: PRICE,
        currency: CURRENCY,
        gateway_base_url: GATEWAY_BASE_URL,
      });
      return;
    }

    // 唯一的业务核心接口
    if (req.method === "GET" && url.pathname === "/execute") {
      await handleExecute(req, res, url);
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
  console.log(`showmethemoney pro backend listening on http://127.0.0.1:${PORT}`);
  console.log(`skill_did=${SKILL_DID}`);
  console.log(`skill_name=${SKILL_NAME}`);
  console.log(`price=${PRICE} ${CURRENCY}`);
});