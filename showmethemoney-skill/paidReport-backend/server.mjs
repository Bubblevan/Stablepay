import { createServer } from "node:http";
import { URL } from "node:url";
import crypto from "node:crypto";
import { config } from "dotenv";
import { readFileSync, existsSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

// 从 .env 文件加载环境变量
config();

const PORT = Number(process.env.PORT || 8788);
const GATEWAY_BASE_URL = process.env.GATEWAY_BASE_URL || "https://ai.wenfu.cn";
const STABLEPAY_API_KEY = process.env.STABLEPAY_API_KEY || "stablepay-dev-key";

const MERCHANT_PROOF_SECRET =
  process.env.MERCHANT_PROOF_SECRET || "replace-this-with-a-long-random-secret";

const ENABLE_DEBUG_ROUTES = process.env.ENABLE_DEBUG_ROUTES === "1";
const INTERNAL_BASE_URL = process.env.INTERNAL_BASE_URL || "http://127.0.0.1:8184";

// Facilitator 配置（x402 标准支付中介）
const FACILITATOR_URL = process.env.FACILITATOR_URL || "https://ai.wenfu.cn";

// 收款钱包配置
const SELLER_ADDRESS = process.env.SELLER_ADDRESS || "2kZGwkLnVdSxjjNueeUQmqBf3tRKMn7y1bbktRZkJWdR";

// USDC 合约地址 (Solana Mainnet)
const USDC_MINT = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v";

// 加载报告配置
let REPORTS_CONFIG = { reports: [] };
try {
  const configPath = join(__dirname, "reports.json");
  if (existsSync(configPath)) {
    REPORTS_CONFIG = JSON.parse(readFileSync(configPath, "utf-8"));
  }
} catch (e) {
  console.error("Failed to load reports.json:", e.message);
}

const REPORTS = new Map(REPORTS_CONFIG.reports.map(r => [r.id, r]));

// 获取报告的 skillDid (x402 标准资源标识)
// 从 reports.json 配置中读取，必须是 did:solana:<pubkey> 格式
function getReportDid(reportId) {
  const report = REPORTS.get(reportId);
  if (report?.skillDid?.startsWith("did:solana:")) {
    return report.skillDid;
  }
  // 回退：使用 SELLER_ADDRESS 生成 DID (不推荐用于生产环境)
  return `did:solana:${SELLER_ADDRESS}`;
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
  if (extra.reportId) {
    console.log(`  Report: ${extra.reportId}`);
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
function buildX402PaymentRequired(report, reportId) {
  const paymentDetails = {
    x402Version: 1,
    accepts: [
      {
        scheme: "exact",
        network: "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp",
        maxAmountRequired: usdcToMinorUnits(report.price),
        payTo: SELLER_ADDRESS,
        asset: USDC_MINT,
        description: `购买报告：${report.title}`,
        resource: `/execute?report_id=${reportId}`,
        maxTimeoutSeconds: 300,
        extra: {
          facilitatorUrl: FACILITATOR_URL,
          currency: report.currency,
          reportId: reportId,
          skillDid: getReportDid(reportId),
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

function buildAccessToken({ agentDid, reportId }) {
  const issuedAt = nowIso();
  const nonce = crypto.randomBytes(12).toString("hex");
  const canonical = [
    "paid-report-access",
    agentDid,
    reportId,
    issuedAt,
    nonce,
  ].join("|");

  const signature = crypto
    .createHmac("sha256", MERCHANT_PROOF_SECRET)
    .update(canonical, "utf8")
    .digest("hex");

  return {
    access_token: `rpt_${nonce}_${signature.slice(0, 24)}`,
    issued_at: issuedAt,
    expires_at: new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString(),
  };
}

// 验证支付状态 (通过 Facilitator)
async function verifyPaymentViaFacilitator(agentDid, skillDid, txId) {
  const url = new URL("/api/v1/verify", FACILITATOR_URL);
  url.searchParams.set("agent_did", agentDid);
  url.searchParams.set("skill_did", skillDid);
  if (txId) {
    url.searchParams.set("tx_id", txId);
  }

  const response = await fetch(url, {
    headers: {
      "X-API-Key": STABLEPAY_API_KEY,
    },
  });

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

function getReportContent(reportId) {
  const report = REPORTS.get(reportId);
  if (!report) return null;

  try {
    const filePath = join(__dirname, report.file);
    if (existsSync(filePath)) {
      return readFileSync(filePath, "utf-8");
    }
  } catch (e) {
    console.error(`Failed to read report ${reportId}:`, e.message);
  }
  return null;
}

// 列出所有报告
function handleListReports(req, res, startTime) {
  const list = Array.from(REPORTS.values()).map(r => ({
    id: r.id,
    skillDid: r.skillDid,
    title: r.title,
    description: r.description,
    price: r.price,
    currency: r.currency,
    author: r.author,
    tags: r.tags,
  }));

  writeJson(res, 200, {
    ok: true,
    reports: list,
    total: list.length,
  });
  logRequest("GET", "/reports", {}, 200, Date.now() - startTime, {
    total: list.length
  });
}

// 获取单个报告信息
function handleGetReport(req, res, url, startTime) {
  const reportId = url.searchParams.get("id");

  if (!reportId) {
    writeJson(res, 400, {
      code: "missing_param",
      message: "id is required",
      expected: "GET /report?id=<report_id>",
    });
    logRequest("GET", "/report", {}, 400, Date.now() - startTime, {
      error: "missing id"
    });
    return;
  }

  const report = REPORTS.get(reportId);
  if (!report) {
    writeJson(res, 404, {
      code: "report_not_found",
      message: `Report '${reportId}' not found`,
    });
    logRequest("GET", "/report", { id: reportId }, 404, Date.now() - startTime, {
      error: `report not found: ${reportId}`
    });
    return;
  }

  writeJson(res, 200, {
    ok: true,
    report: {
      id: report.id,
      skillDid: report.skillDid,
      title: report.title,
      description: report.description,
      price: report.price,
      currency: report.currency,
      author: report.author,
      tags: report.tags,
    },
  });
  logRequest("GET", "/report", { id: reportId }, 200, Date.now() - startTime, {
    title: report.title
  });
}

// x402 标准核心业务：执行购买验证并返回报告
async function handleExecute(req, res, url, headers, startTime) {
  const agentDid = url.searchParams.get("agent_did");
  const reportId = url.searchParams.get("report_id");

  // x402 标准：检查 Payment-Signature header（支付凭证）
  const paymentSignature = headers["payment-signature"] || headers["X-Payment-Signature"];
  const paymentData = parsePaymentSignature(paymentSignature);

  const params = {};
  if (agentDid) params.agent_did = agentDid.substring(0, 20) + "...";
  if (reportId) params.report_id = reportId;

  if (!agentDid || !reportId) {
    writeJson(res, 400, {
      code: "missing_param",
      message: "agent_did and report_id are required",
      expected: "GET /execute?agent_did=<did>&report_id=<id>",
    });
    logRequest("GET", "/execute", params, 400, Date.now() - startTime, {
      error: "missing params"
    });
    return;
  }

  const report = REPORTS.get(reportId);
  if (!report) {
    writeJson(res, 404, {
      code: "report_not_found",
      message: `Report '${reportId}' not found`,
      available_reports: Array.from(REPORTS.keys()),
    });
    logRequest("GET", "/execute", params, 404, Date.now() - startTime, {
      error: `report not found: ${reportId}`
    });
    return;
  }

  const reportDid = getReportDid(reportId);

  // x402 标准：如果提供了 Payment-Signature，先尝试结算支付
  if (paymentData) {
    console.log("[x402] Payment-Signature detected, attempting settlement...");
    const settleResult = await settlePaymentViaFacilitator({
      agent_did: agentDid,
      skill_did: reportDid,
      price: usdcToMinorUnits(report.price),
      currency: report.currency,
      signature: paymentData.signature,
      sign_nonce: paymentData.nonce,
      sign_timestamp: paymentData.timestamp,
    });

    if (settleResult.status >= 200 && settleResult.status < 300) {
      console.log("[x402] Payment settled successfully:", settleResult.body);
      // 支付成功，继续验证并返回内容
    } else {
      console.log("[x402] Payment settlement failed:", settleResult.body);
      // 结算失败，继续检查是否已有购买记录
    }
  }

  // 验证购买状态（通过 Facilitator）
  const verify = await verifyPaymentViaFacilitator(agentDid, reportDid, paymentData?.txId);

  if (verify.status >= 400) {
    writeJson(res, 502, {
      code: "verify_failed",
      message: "failed to verify purchase state with facilitator",
      facilitator_verify: verify,
    });
    logRequest("GET", "/execute", params, 502, Date.now() - startTime, {
      agentDid: agentDid.substring(0, 20) + "...",
      reportId,
      error: `verify failed: ${verify.status}`
    });
    return;
  }

  const purchased = Boolean(verify.body?.data?.purchased || verify.body?.purchased);

  if (!purchased) {
    // x402 标准：返回 402 并设置 Payment-Required header
    const paymentRequiredHeader = buildX402PaymentRequired(report, reportId);

    writeJson(res, 402, {
      x402Version: 1,
      accepts: [
        {
          scheme: "exact",
          network: "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp",
          maxAmountRequired: usdcToMinorUnits(report.price),
          payTo: SELLER_ADDRESS,
          asset: USDC_MINT,
          description: `购买报告：${report.title}`,
          resource: `/execute?report_id=${reportId}`,
          maxTimeoutSeconds: 300,
          extra: {
            facilitatorUrl: FACILITATOR_URL,
            currency: report.currency,
            reportId: reportId,
            skillDid: reportDid,
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
      reportId,
      purchased: false,
      paymentSignature: !!paymentData
    });
    return;
  }

  // 已购买，返回报告内容
  const content = getReportContent(reportId);
  const accessToken = buildAccessToken({ agentDid, reportId });

  if (!content) {
    writeJson(res, 500, {
      code: "content_not_available",
      message: "Report content is not available at the moment",
    });
    logRequest("GET", "/execute", params, 500, Date.now() - startTime, {
      agentDid: agentDid.substring(0, 20) + "...",
      reportId,
      error: "content not available"
    });
    return;
  }

  // x402 标准：返回 Payment-Response header
  const txId = verify.body?.data?.tx_id || verify.body?.tx_id;
  const txHash = verify.body?.data?.tx_hash || verify.body?.tx_hash;
  const paymentResponseHeader = txId ? buildX402PaymentResponse(txId, txHash) : null;

  const responseHeaders = {};
  if (paymentResponseHeader) {
    responseHeaders["Payment-Response"] = paymentResponseHeader;
  }

  writeJson(res, 200, {
    ok: true,
    product: "paid-report",
    x402Version: 1,
    access: {
      agent_did: agentDid,
      report_id: reportId,
      verified_by_backend: true,
      verified_at: nowIso(),
      access_token: accessToken,
    },
    report: {
      id: report.id,
      title: report.title,
      author: report.author,
      tags: report.tags,
    },
    content: {
      format: "markdown",
      text: content,
    },
    verify_snapshot: verify.body,
  }, responseHeaders);

  logRequest("GET", "/execute", params, 200, Date.now() - startTime, {
    agentDid: agentDid.substring(0, 20) + "...",
    reportId,
    purchased: true,
    accessToken: accessToken.access_token.substring(0, 20) + "..."
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

    // Health check
    if (req.method === "GET" && url.pathname === "/healthz") {
      writeJson(res, 200, {
        ok: true,
        service: "paid-report-backend",
        gateway_base_url: GATEWAY_BASE_URL,
        facilitator_url: FACILITATOR_URL,
        x402_version: 1,
        available_reports: Array.from(REPORTS.keys()).length,
      });
      logRequest("GET", "/healthz", {}, 200, Date.now() - startTime);
      return;
    }

    // 列出所有报告
    if (req.method === "GET" && url.pathname === "/reports") {
      handleListReports(req, res, startTime);
      return;
    }

    // 获取单个报告信息
    if (req.method === "GET" && url.pathname === "/report") {
      handleGetReport(req, res, url, startTime);
      return;
    }

    // x402 标准核心业务接口
    if (req.method === "GET" && url.pathname === "/execute") {
      await handleExecute(req, res, url, headers, startTime);
      return;
    }

    // 调试接口
    if (ENABLE_DEBUG_ROUTES && req.method === "GET" && url.pathname === "/agent/transactions") {
      const agentDid = url.searchParams.get("agent_did");
      if (!agentDid) {
        writeJson(res, 400, { code: "missing_param", message: "agent_did is required" });
        return;
      }
      const url2 = new URL("/internal/transactions", INTERNAL_BASE_URL);
      url2.searchParams.set("did", agentDid);
      url2.searchParams.set("type", "1");
      const response = await fetch(url2);
      writeJson(res, response.status, await readJson(response));
      return;
    }

    writeJson(res, 404, {
      code: "not_found",
      message: "route not found",
    });
  } catch (error) {
    writeJson(res, 500, {
      code: "backend_error",
      message: error instanceof Error ? error.message : String(error),
    });
  }
});

server.listen(PORT, () => {
  console.log(`Paid Report backend (x402 compliant) listening on http://127.0.0.1:${PORT}`);
  console.log(`Facilitator: ${FACILITATOR_URL}`);
  console.log(`Seller Address: ${SELLER_ADDRESS}`);
  console.log(`Available reports: ${Array.from(REPORTS.keys()).join(", ")}`);
});
