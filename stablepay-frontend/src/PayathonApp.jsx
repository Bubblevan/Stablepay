import { useEffect, useRef, useState } from "react";
import "./payathon.css";

const sample = {
  activity: "社团迎新分享会",
  school: "",
  budget: "500",
  budget_evidence: "演示预算记录（非真实审批材料）",
  approved: true,
  items: [
    {
      name: "活动横幅",
      requested_spec: "3m × 0.8m",
      quoted_spec: "3m × 0.8m",
      quantity: 1,
      unit_price: "180",
      quote_source: "演示报价单",
      invoice_confirmed: true,
    },
    {
      name: "宣传展架",
      requested_spec: "80cm × 180cm",
      quoted_spec: "60cm × 160cm",
      quantity: 2,
      unit_price: "68",
      quote_source: "演示报价单",
      invoice_confirmed: true,
    },
    {
      name: "饮用水",
      requested_spec: "24瓶/箱",
      quoted_spec: "24瓶/箱",
      quantity: 4,
      unit_price: "35",
      quote_source: "演示报价单",
      invoice_confirmed: false,
    },
    {
      name: "签到桌牌",
      requested_spec: "亚克力 A5",
      quoted_spec: "亚克力 A5",
      quantity: 4,
      unit_price: "20",
      quote_source: "演示报价单",
      invoice_confirmed: true,
    },
  ],
};
const yuan = (value) =>
  new Intl.NumberFormat("zh-CN", { style: "currency", currency: "CNY" }).format(
    value / 100,
  );
const stateLabels = {
  AWAITING_PAYMENT: "等待支付确认",
  DELIVERED: "已交付报告",
  CLOSED: "订单已关闭",
};
const eventDetail = (event, purchase) => {
  if (event.type === "PAYMENT_CREATED") return "支付订单已创建，金额已固定";
  if (event.type === "PAYMENT_VERIFIED")
    return purchase.quote.mode === "local"
      ? "本地模拟结算已确认（不扣款）"
      : "支付宝沙箱结果已核验";
  return event.detail;
};
async function request(path, body, key) {
  const response = await fetch(`/api/mvp/${path}`, {
    method: body === undefined ? "GET" : "POST",
    credentials: "same-origin",
    headers: {
      "Content-Type": "application/json",
      "X-Requested-With": "StablePay",
      ...(key ? { "Idempotency-Key": key } : {}),
    },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  });
  let result;
  try {
    result = await response.json();
  } catch {
    throw new Error("服务暂不可用，请确认采购 Runtime 与支付服务已经启动。");
  }
  if (!response.ok) throw new Error(result.error || "请求未完成，请稍后重试。");
  return result;
}

export default function PayathonApp() {
  const [input, setInput] = useState(() => structuredClone(sample));
  const [prompt, setPrompt] = useState(
    "帮我检查迎新采购清单，重点查预算、规格和票据准备。",
  );
  const [serviceBudget, setServiceBudget] = useState("1.00");
  const [status, setStatus] = useState(null);
  const [catalog, setCatalog] = useState([]);
  const [plan, setPlan] = useState(null);
  const [quote, setQuote] = useState(null);
  const [accepted, setAccepted] = useState(false);
  const [purchase, setPurchase] = useState(null);
  const [history, setHistory] = useState([]);
  const [cashier, setCashier] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const approvalKey = useRef("");
  const outputRef = useRef(null);
  const revealOutput = () => {
    if (window.matchMedia("(max-width: 800px)").matches)
      requestAnimationFrame(() =>
        outputRef.current?.scrollIntoView({
          behavior: window.matchMedia("(prefers-reduced-motion: reduce)")
            .matches
            ? "auto"
            : "smooth",
          block: "start",
        }),
      );
  };

  useEffect(() => {
    document.title = "StablePay · 可信采购任务助手";
    let active = true;
    async function load() {
      try {
        const currentStatus = await request("status"); // Establish one session before parallel reads.
        const [services, purchases] = await Promise.all([
          request("catalog"),
          request("purchases"),
        ]);
        if (active) {
          setStatus(currentStatus);
          setCatalog(services);
          setHistory(purchases);
          setPurchase(purchases[0] || null);
        }
      } catch (cause) {
        if (active) setError(cause.message);
      }
    }
    load();
    return () => {
      active = false;
    };
  }, []);

  const invalidate = () => {
    setPlan(null);
    setQuote(null);
    setAccepted(false);
    setPurchase(null);
    setCashier("");
  };
  const edit = (field, value) => {
    setInput((current) => ({ ...current, [field]: value }));
    invalidate();
  };
  const editItem = (index, field, value) => {
    setInput((current) => ({
      ...current,
      items: current.items.map((item, i) =>
        i === index ? { ...item, [field]: value } : item,
      ),
    }));
    invalidate();
  };
  const perform = async (action) => {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      await action();
    } catch (cause) {
      setError(cause.message);
    } finally {
      setBusy(false);
    }
  };
  const refreshHistory = async () => {
    setHistory(await request("purchases"));
  };
  const createPlan = () =>
    perform(async () => {
      const result = await request("plans", {
        prompt,
        service_budget: serviceBudget,
        input,
      });
      setPlan(result);
      setQuote(null);
      setPurchase(null);
      setAccepted(false);
      setCashier("");
      revealOutput();
    });
  const getQuote = (productId) =>
    perform(async () => {
      const result = await request("quotes", {
        plan_id: plan.id,
        product_id: productId,
      });
      setQuote(result);
      setAccepted(false);
      approvalKey.current = crypto.randomUUID();
      revealOutput();
    });
  const approve = () =>
    perform(async () => {
      const result = await request(
        "purchases",
        { quote_id: quote.id, quote_hash: quote.hash, accepted },
        approvalKey.current,
      );
      setPurchase(result);
      setCashier("");
      await refreshHistory();
      revealOutput();
    });
  const reconcile = (simulate = false) =>
    perform(async () => {
      const result = await request(
        `purchases/${purchase.id}/${simulate ? "simulate" : "reconcile"}`,
        {},
      );
      setPurchase(result);
      await refreshHistory();
      revealOutput();
    });
  const prepareCheckout = () =>
    perform(async () => {
      const result = await request(`purchases/${purchase.id}/checkout`, {});
      if (
        result.mode !== "alipay_sandbox" ||
        !result.cashier_url.startsWith(
          "https://openapi-sandbox.dl.alipaydev.com/gateway.do?",
        )
      )
        throw new Error("收银台环境不匹配，请重新查询订单。");
      setCashier(result.cashier_url);
    });
  const delivery = purchase?.delivery;

  return (
    <div className="pay-app">
      <header className="pay-header">
        <a href="/payathon" className="pay-brand">
          <span>S</span>
          <div>
            StablePay<small>可信采购任务助手</small>
          </div>
        </a>
        <nav>
          <a href="/campus">校园采购预审</a>
          <a
            href="https://aipay.alipay.com/community/events/payathon-2026"
            target="_blank"
            rel="noreferrer"
          >
            PAYATHON 2026 ↗
          </a>
        </nav>
        <span className="pay-mode">
          {status?.payment_mode === "alipay_sandbox"
            ? "支付宝沙箱 · 不使用真实资金"
            : "本地演示 · 不扣款"}
        </span>
      </header>
      <main className="pay-shell">
        <div className="pay-intro">
          <div>
            <span className="pay-kicker">从需求到交付，每一步由你掌控</span>
            <h1>
              先看清方案，
              <br />
              再确认每一笔花费。
            </h1>
            <p>
              告诉助手要检查什么。获取明确报价、确认服务费，付款核验后领取采购报告和交易回执。
            </p>
          </div>
          <div className="pay-flow">
            <span>01 整理需求</span>
            <span>02 确认报价</span>
            <span>03 支付与交付</span>
          </div>
        </div>
        {error && (
          <div className="pay-error" role="alert">
            {error}
          </div>
        )}
        <div className="pay-workspace">
          <fieldset className="pay-input-column" disabled={busy} aria-label="活动采购资料">
            <section className="pay-panel">
              <div className="pay-section-heading">
                <h2>这次需要助手做什么？</h2>
                <span>任务资料</span>
              </div>
              <label className="pay-label">
                任务描述
                <textarea
                  value={prompt}
                  onChange={(event) => {
                    setPrompt(event.target.value);
                    invalidate();
                  }}
                  rows={3}
                />
              </label>
              <div className="pay-fields">
                <label className="pay-label">
                  活动名称
                  <input
                    value={input.activity}
                    onChange={(event) => edit("activity", event.target.value)}
                  />
                </label>
                <label className="pay-label">
                  本次服务费上限（元）
                  <input
                    type="number"
                    min="0.01"
                    max="100"
                    step="0.01"
                    value={serviceBudget}
                    onChange={(event) => {
                      setServiceBudget(event.target.value);
                      invalidate();
                    }}
                  />
                </label>
                <label className="pay-label">
                  活动采购预算（元）
                  <input
                    type="number"
                    min="0.01"
                    step="0.01"
                    value={input.budget}
                    onChange={(event) => edit("budget", event.target.value)}
                  />
                </label>
                <label className="pay-label">
                  预算审批依据
                  <input
                    value={input.budget_evidence}
                    onChange={(event) =>
                      edit("budget_evidence", event.target.value)
                    }
                    placeholder="文件名 / 链接 / 记录编号"
                  />
                </label>
              </div>
              <label className="pay-check">
                <input
                  type="checkbox"
                  checked={input.approved}
                  onChange={(event) => edit("approved", event.target.checked)}
                />
                活动采购预算已取得负责人确认（由你据实填写）
              </label>
              <p className="pay-note">
                服务费是购买核验报告的费用，与活动物资采购预算分别管理。默认清单为演示材料，尚未绑定具体学校制度。
              </p>
            </section>
            <section className="pay-panel">
              <div className="pay-section-heading">
                <h2>采购明细</h2>
                <span>{input.items.length} 项 · 可直接修改</span>
              </div>
              <div className="pay-items">
                {input.items.map((item, index) => (
                  <article key={index} className="pay-item">
                    <div className="pay-item-title">
                      <span>{String(index + 1).padStart(2, "0")}</span>
                      <input
                        aria-label={`物资 ${index + 1} 名称`}
                        value={item.name}
                        onChange={(event) =>
                          editItem(index, "name", event.target.value)
                        }
                      />
                      <button
                        type="button"
                        onClick={() => {
                          edit(
                            "items",
                            input.items.filter((_, i) => i !== index),
                          );
                        }}
                        aria-label={`移除${item.name}`}
                      >
                        移除
                      </button>
                    </div>
                    <div className="pay-item-fields">
                      <label className="pay-label">
                        需求规格
                        <input
                          value={item.requested_spec}
                          onChange={(event) =>
                            editItem(
                              index,
                              "requested_spec",
                              event.target.value,
                            )
                          }
                        />
                      </label>
                      <label className="pay-label">
                        报价规格
                        <input
                          value={item.quoted_spec}
                          onChange={(event) =>
                            editItem(index, "quoted_spec", event.target.value)
                          }
                        />
                      </label>
                      <label className="pay-label">
                        数量
                        <input
                          type="number"
                          min="1"
                          step="1"
                          value={item.quantity}
                          onChange={(event) =>
                            editItem(
                              index,
                              "quantity",
                              Number(event.target.value),
                            )
                          }
                        />
                      </label>
                      <label className="pay-label">
                        单价（元）
                        <input
                          type="number"
                          min="0.01"
                          step="0.01"
                          value={item.unit_price}
                          onChange={(event) =>
                            editItem(index, "unit_price", event.target.value)
                          }
                        />
                      </label>
                    </div>
                    <label className="pay-label pay-source">
                      报价来源
                      <input
                        value={item.quote_source}
                        onChange={(event) =>
                          editItem(index, "quote_source", event.target.value)
                        }
                      />
                    </label>
                    <label className="pay-check">
                      <input
                        type="checkbox"
                        checked={item.invoice_confirmed}
                        onChange={(event) =>
                          editItem(
                            index,
                            "invoice_confirmed",
                            event.target.checked,
                          )
                        }
                      />
                      供应商已确认所需票据条件
                    </label>
                  </article>
                ))}
              </div>
              <button
                className="pay-add"
                disabled={input.items.length >= 50}
                onClick={() =>
                  edit("items", [
                    ...input.items,
                    {
                      name: "",
                      requested_spec: "",
                      quoted_spec: "",
                      quantity: 1,
                      unit_price: "",
                      quote_source: "",
                      invoice_confirmed: false,
                    },
                  ])
                }
              >
                ＋ 添加采购项
              </button>
              <button
                className="pay-button primary pay-full"
                disabled={busy || !status}
                onClick={createPlan}
              >
                {busy ? "处理中…" : "整理需求并发现服务 →"}
              </button>
            </section>
          </fieldset>
          <aside className="pay-output-column" ref={outputRef}>
            {!plan && !purchase && (
              <section className="pay-panel pay-placeholder">
                <div>✓</div>
                <h2>一份能回查的采购报告</h2>
                <p>
                  先整理左侧资料，再选择服务。每次报价都会列明商户、服务、金额和有效期，得到你的确认后才创建支付订单。
                </p>
                <ul>
                  <li>金额来自服务端固定报价</li>
                  <li>修改资料需要重新获取报价</li>
                  <li>支付未核验时不会交付报告</li>
                </ul>
              </section>
            )}
            {plan && !purchase && (
              <section className="pay-panel">
                <div className="pay-section-heading">
                  <h2>助手找到的服务</h2>
                  <span>
                    {plan.planner_mode === "llm"
                      ? "模型建议"
                      : plan.planner_mode === "rules"
                        ? "规则规划"
                        : "规则回退"}
                  </span>
                </div>
                <p className="pay-rationale">{plan.explanation}</p>
                <div className="pay-products">
                  {catalog.map((product) => (
                    <article
                      key={product.id}
                      className={
                        plan.recommended_product === product.id
                          ? "recommended"
                          : ""
                      }
                    >
                      <div>
                        <h3>{product.name}</h3>
                        <strong>{yuan(product.amount_minor)}</strong>
                      </div>
                      <p>{product.description}</p>
                      <small>
                        {product.merchant}
                        {plan.recommended_product === product.id
                          ? " · 本次建议"
                          : ""}
                      </small>
                      <button
                        disabled={busy}
                        className="pay-button secondary"
                        onClick={() => getQuote(product.id)}
                      >
                        获取此服务报价
                      </button>
                    </article>
                  ))}
                </div>
              </section>
            )}
            {quote && !purchase && (
              <section className="pay-panel">
                <div className="pay-section-heading">
                  <h2>请确认这份报价</h2>
                  <span>5 分钟有效</span>
                </div>
                <dl className="pay-quote">
                  <div>
                    <dt>服务</dt>
                    <dd>{quote.product.name}</dd>
                  </div>
                  <div>
                    <dt>提供方</dt>
                    <dd>{quote.product.merchant}</dd>
                  </div>
                  <div>
                    <dt>应付服务费</dt>
                    <dd className="pay-price">
                      {yuan(quote.product.amount_minor)}
                    </dd>
                  </div>
                  <div>
                    <dt>有效至</dt>
                    <dd>
                      {new Date(quote.expires_at).toLocaleTimeString("zh-CN")}
                    </dd>
                  </div>
                  <div>
                    <dt>支付环境</dt>
                    <dd>
                      {quote.mode === "local"
                        ? "本地模拟，不会扣款"
                        : "支付宝电脑网站支付沙箱"}
                    </dd>
                  </div>
                </dl>
                <label className="pay-check">
                  <input
                    type="checkbox"
                    checked={accepted}
                    disabled={busy}
                    onChange={(event) => setAccepted(event.target.checked)}
                  />
                  我确认服务、商户和费用，按此报价创建订单
                </label>
                <button
                  className="pay-button primary pay-full"
                  disabled={busy || !accepted}
                  onClick={approve}
                >
                  确认并创建订单
                </button>
              </section>
            )}
            {purchase && (
              <section className="pay-panel">
                <div className="pay-section-heading">
                  <h2>{stateLabels[purchase.status] || purchase.status}</h2>
                  <span>
                    {purchase.quote.mode === "local"
                      ? "本地模拟订单"
                      : "支付宝沙箱订单"}
                  </span>
                </div>
                <p className="pay-order-name">
                  {purchase.quote.product.name}
                  <strong>{yuan(purchase.quote.product.amount_minor)}</strong>
                </p>
                <div className="pay-timeline">
                  {purchase.events.map((event, index) => (
                    <div key={index}>
                      <span />
                      <p>
                        {eventDetail(event, purchase)}
                        <small>
                          {new Date(event.at).toLocaleString("zh-CN")}
                        </small>
                      </p>
                    </div>
                  ))}
                </div>
                {purchase.status === "AWAITING_PAYMENT" && (
                  <div className="pay-payment-actions">
                    {purchase.quote.mode === "local" ? (
                      <button
                        className="pay-button primary pay-full"
                        disabled={busy}
                        onClick={() => reconcile(true)}
                      >
                        模拟结算并生成报告（不扣款）
                      </button>
                    ) : (
                      <>
                        <button
                          className="pay-button primary pay-full"
                          disabled={busy}
                          onClick={prepareCheckout}
                        >
                          准备支付宝沙箱收银台
                        </button>
                        {cashier && (
                          <a
                            className="pay-button secondary pay-full"
                            href={cashier}
                            target="_blank"
                            rel="noreferrer"
                          >
                            前往沙箱付款 ↗
                          </a>
                        )}
                      </>
                    )}
                    <button
                      className="pay-button secondary pay-full"
                      disabled={busy}
                      onClick={() => reconcile(false)}
                    >
                      查询支付结果
                    </button>
                  </div>
                )}
                {delivery && (
                  <div className="pay-delivery">
                    <h3>{delivery.report.activity} · 核验结果</h3>
                    <div className="pay-report-totals">
                      <div>
                        活动预算
                        <strong>{yuan(delivery.report.budget_minor)}</strong>
                      </div>
                      <div>
                        物资合计
                        <strong>{yuan(delivery.report.total_minor)}</strong>
                      </div>
                    </div>
                    {delivery.report.findings.length ? (
                      delivery.report.findings.map((finding, index) => (
                        <article
                          className={`pay-finding ${finding.severity}`}
                          key={index}
                        >
                          <h4>{finding.title}</h4>
                          <p>{finding.evidence}</p>
                          <div>{finding.action}</div>
                        </article>
                      ))
                    ) : (
                      <p className="pay-clear">
                        当前输入未触发所选服务的检查项。
                      </p>
                    )}
                    <p className="pay-note">{delivery.report.scope}</p>
                    <a
                      className="pay-button secondary pay-full"
                      href={`/api/mvp/purchases/${purchase.id}/receipt`}
                    >
                      下载报告与交易回执 ↓
                    </a>
                    <small className="pay-hash">
                      内容摘要 {delivery.content_hash.slice(0, 16)}…
                    </small>
                  </div>
                )}
              </section>
            )}
            {history.length > 0 && (
              <section className="pay-panel">
                <div className="pay-section-heading">
                  <h2>本次浏览器的订单</h2>
                  <span>{history.length} 笔</span>
                </div>
                <div className="pay-history">
                  {history.map((entry) => (
                    <button
                      key={entry.id}
                      disabled={busy}
                      onClick={() => {
                        setPurchase(entry);
                        setCashier("");
                        setQuote(null);
                      }}
                    >
                      <span>
                        {entry.quote.product.name}
                        <small>
                          {new Date(entry.approved_at).toLocaleString("zh-CN")}
                        </small>
                      </span>
                      <span>
                        {stateLabels[entry.status]}
                        <small>{yuan(entry.quote.product.amount_minor)}</small>
                      </span>
                    </button>
                  ))}
                </div>
              </section>
            )}
          </aside>
        </div>
        <footer className="pay-footer">
          <span>StablePay · PAYATHON MVP</span>
          <span>
            当前仅开放本地演示与支付宝沙箱适配，正式交易权限尚待开通。
          </span>
        </footer>
      </main>
    </div>
  );
}
