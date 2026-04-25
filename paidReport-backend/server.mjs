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

// 生成报告 DID
function getReportDid(reportId) {
  return `did:report:${reportId}`;
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

function writeJson(res, status, payload) {
  res.statusCode = status;
  res.setHeader("Content-Type", "application/json; charset=utf-8");
  res.end(JSON.stringify(payload, null, 2));
}

function nowIso() {
  return new Date().toISOString();
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

async function fetchVerify(agentDid, reportId) {
  const reportDid = getReportDid(reportId);
  const url = new URL("/api/v1/verify", GATEWAY_BASE_URL);
  url.searchParams.set("agent_did", agentDid);
  url.searchParams.set("skill_did", reportDid);

  const response = await fetch(url, {
    headers: {
      "X-API-Key": STABLEPAY_API_KEY,
    },
  });

  return { status: response.status, body: await readJson(response) };
}

async function fetchPayRequirement(agentDid, report) {
  const reportDid = getReportDid(report.id);
  const url = new URL("/api/v1/pay/require", GATEWAY_BASE_URL);
  url.searchParams.set("skill_did", reportDid);
  url.searchParams.set("agent_did", agentDid);
  url.searchParams.set("skill_name", report.title);
  url.searchParams.set("price", report.price);
  url.searchParams.set("currency", report.currency);
  url.searchParams.set("message", `购买报告：${report.title}`);

  const response = await fetch(url);
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
function handleListReports(req, res) {
  const list = Array.from(REPORTS.values()).map(r => ({
    id: r.id,
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
}

// 获取单个报告信息
function handleGetReport(req, res, url) {
  const reportId = url.searchParams.get("id");

  if (!reportId) {
    writeJson(res, 400, {
      code: "missing_param",
      message: "id is required",
      expected: "GET /report?id=<report_id>",
    });
    return;
  }

  const report = REPORTS.get(reportId);
  if (!report) {
    writeJson(res, 404, {
      code: "report_not_found",
      message: `Report '${reportId}' not found`,
    });
    return;
  }

  writeJson(res, 200, {
    ok: true,
    report: {
      id: report.id,
      title: report.title,
      description: report.description,
      price: report.price,
      currency: report.currency,
      author: report.author,
      tags: report.tags,
    },
  });
}

// 核心业务：执行购买验证并返回报告
async function handleExecute(req, res, url) {
  const agentDid = url.searchParams.get("agent_did");
  const reportId = url.searchParams.get("report_id");

  if (!agentDid || !reportId) {
    writeJson(res, 400, {
      code: "missing_param",
      message: "agent_did and report_id are required",
      expected: "GET /execute?agent_did=<did>&report_id=<id>",
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
    return;
  }

  // 验证购买状态
  const verify = await fetchVerify(agentDid, reportId);

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
    const requirement = await fetchPayRequirement(agentDid, report);
    const pr = requirement.body?.data || requirement.body || {};

    writeJson(res, 402, {
      code: 402,
      message: "Payment Required",
      agent_did: agentDid,
      report: {
        id: report.id,
        title: report.title,
        description: report.description,
        price: report.price,
        currency: report.currency,
      },
      payment_endpoint: pr.payment_endpoint || "/api/v1/pay",
      merchant_backend: {
        endpoint: "/execute",
        report_id: reportId,
      },
      payment_requirement: requirement.body,
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
    return;
  }

  writeJson(res, 200, {
    ok: true,
    product: "paid-report",
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
  });
}

const server = createServer(async (req, res) => {
  try {
    const url = new URL(
      req.url || "/",
      `http://${req.headers.host || `127.0.0.1:${PORT}`}`,
    );

    // Health check
    if (req.method === "GET" && url.pathname === "/healthz") {
      writeJson(res, 200, {
        ok: true,
        service: "paid-report-backend",
        gateway_base_url: GATEWAY_BASE_URL,
        available_reports: Array.from(REPORTS.keys()).length,
      });
      return;
    }

    // 列出所有报告
    if (req.method === "GET" && url.pathname === "/reports") {
      handleListReports(req, res);
      return;
    }

    // 获取单个报告信息
    if (req.method === "GET" && url.pathname === "/report") {
      handleGetReport(req, res, url);
      return;
    }

    // 核心业务接口：执行购买验证
    if (req.method === "GET" && url.pathname === "/execute") {
      await handleExecute(req, res, url);
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
  console.log(`Paid Report backend listening on http://127.0.0.1:${PORT}`);
  console.log(`Available reports: ${Array.from(REPORTS.keys()).join(", ")}`);
});
