import { useEffect, useMemo, useRef, useState } from "react";
import {
  workspaceKey,
  emptyProof,
  requirementsExample,
  quotationsExample,
  newItem,
  initialDraft,
  moneyMinor,
  previewTotal,
  reviewPayload,
  sourceExcerpt,
  isCurrentReview,
} from "./campusModel.mjs";
import "./campus-agent.css";

const currency = new Intl.NumberFormat("zh-CN", {
  style: "currency",
  currency: "CNY",
});
const money = (value) =>
  value === null ? "待补齐" : currency.format(value / 100);
const fieldNames = {
  name: "名称",
  activity: "活动",
  budget: "预算",
  requested_spec: "需求规格",
  quoted_spec: "报价规格",
  quantity: "数量",
  unit_price: "单价",
  invoice_statement: "票据原文",
};
async function api(path, body) {
  const response = await fetch("/api/mvp/" + path, {
    method: body === undefined ? "GET" : "POST",
    credentials: "same-origin",
    headers: {
      "Content-Type": "application/json",
      "X-Requested-With": "StablePay",
    },
    signal: AbortSignal.timeout(45000),
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  });
  const data = await response.json();
  if (!response.ok) throw new Error(data.error || "服务暂不可用，请重试。");
  return data;
}
function loadWorkspace() {
  try {
    const raw = localStorage.getItem(workspaceKey);
    if (raw && raw.length < 2000000) {
      const value = JSON.parse(raw);
      if (
        value.draft &&
        Array.isArray(value.draft.items) &&
        value.draft.items.length <= 50 &&
        typeof value.requirements === "string" &&
        typeof value.quotations === "string"
      )
        return value;
    }
  } catch {
    /* Private browsing can disable storage. */
  }
  return {
    requirements: requirementsExample,
    quotations: quotationsExample,
    draft: initialDraft(),
    extraction: null,
    review: null,
  };
}
function Field({ label, hint, children, className = "" }) {
  return (
    <label className={"campus-field " + className}>
      <span>{label}</span>
      {children}
      {hint && <small>{hint}</small>}
    </label>
  );
}
function Proof({ label, value, onChange }) {
  const proof = value || emptyProof();
  return (
    <details className="campus-proof">
      <summary>
        {label} ·{" "}
        {
          {
            declared: "用户声明",
            provided: "有材料，未核验",
            human_verified: "人工核验记录",
          }[proof.state || "declared"]
        }
      </summary>
      <div className="campus-field-grid">
        <Field label={label + "状态"}>
          <select
            value={proof.state || "declared"}
            onChange={(e) =>
              onChange({
                ...proof,
                state: e.target.value,
                reviewed_by: "",
                reviewed_at: "",
              })
            }
          >
            <option value="declared">用户声明</option>
            <option value="provided">有证据但未核验</option>
            <option value="human_verified">已经人工核验（用户记录）</option>
          </select>
        </Field>
        <Field label={label + "引用"} hint="文件名或链接不会被系统自动验证。">
          <input
            value={proof.reference || ""}
            onChange={(e) =>
              onChange({
                ...proof,
                reference: e.target.value,
                reviewed_by: "",
                reviewed_at: "",
              })
            }
          />
        </Field>
        {proof.state === "human_verified" && (
          <>
            <Field label={label + "复核人"}>
              <input
                value={proof.reviewed_by}
                onChange={(e) =>
                  onChange({ ...proof, reviewed_by: e.target.value })
                }
              />
            </Field>
            <Field
              label={label + "复核时间"}
              hint="须填写真实复核时间，系统不自动代签。"
            >
              <input
                type="datetime-local"
                value={proof.reviewed_at ? localDate(proof.reviewed_at) : ""}
                onChange={(e) =>
                  onChange({
                    ...proof,
                    reviewed_at: e.target.value
                      ? new Date(e.target.value).toISOString()
                      : "",
                  })
                }
              />
            </Field>
          </>
        )}
      </div>
    </details>
  );
}
function localDate(raw) {
  const date = new Date(raw);
  return Number.isNaN(date.getTime())
    ? ""
    : new Date(date.getTime() - date.getTimezoneOffset() * 60000)
        .toISOString()
        .slice(0, 16);
}

export default function CampusProcurementApp() {
  const [workspace, setWorkspace] = useState(loadWorkspace);
  const { requirements, quotations, draft, extraction, review } = workspace;
  const [status, setStatus] = useState(null);
  const [policies, setPolicies] = useState([]);
  const [busy, setBusy] = useState("");
  const [notice, setNotice] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const [storageFailed, setStorageFailed] = useState(false);
  const current = useRef("");
  current.current = JSON.stringify(workspace);
  useEffect(() => {
    const oldTitle = document.title;
    document.title = "校园采购守门员 · 可追溯 AI 预审";
    api("campus/status")
      .then((value) => {
        setStatus(value);
        return api("campus/policies")
          .then(setPolicies)
          .catch(() => {});
      })
      .catch(() => setStatus({ unavailable: true }));
    const saved = loadWorkspace();
    if (saved.review?.id)
      api("campus/reviews/" + saved.review.id)
        .then((value) =>
          setWorkspace((w) =>
            w.review?.id === value.id ? { ...w, review: value } : w,
          ),
        )
        .catch(() => {
          setWorkspace((w) => ({ ...w, review: null }));
          setNotice("上次预审记录无法恢复，请重新确认并预审。");
        });
    return () => {
      document.title = oldTitle;
    };
  }, []);
  useEffect(() => {
    try {
      localStorage.setItem(workspaceKey, JSON.stringify(workspace));
      setStorageFailed(false);
    } catch {
      setStorageFailed(true);
    }
  }, [workspace]);
  const change = (updates) => {
    setWorkspace((w) => ({ ...w, ...updates }));
    setConfirmed(false);
    setNotice("");
  };
  const changeDraft = (key, value) => {
    setWorkspace((w) => ({
      ...w,
      draft: {
        ...w.draft,
        [key]: value,
        ...(key === "budget" || key === "budget_evidence"
          ? { budget_proof: emptyProof() }
          : {}),
        ...(key === "approved" ? { approval_proof: emptyProof() } : {}),
      },
    }));
    setConfirmed(false);
    setNotice("");
  };
  const changeItem = (index, key, value) => {
    setWorkspace((w) => ({
      ...w,
      draft: {
        ...w.draft,
        items: w.draft.items.map((item, i) =>
          i !== index
            ? item
            : {
                ...item,
                [key]: value,
                ...([
                  "name",
                  "quoted_spec",
                  "unit_price",
                  "quote_source",
                  "quantity",
                  "requested_spec",
                ].includes(key)
                  ? { quote_proof: emptyProof(item.quote_proof?.reference) }
                  : {}),
                ...(key === "invoice_confirmed"
                  ? { invoice_proof: emptyProof(item.invoice_proof?.reference) }
                  : {}),
              },
        ),
      },
    }));
    setConfirmed(false);
    setNotice("");
  };
  const total = useMemo(() => previewTotal(draft.items), [draft.items]);
  const sourceChanged =
    extraction &&
    (requirements !== extraction.source.requirements ||
      quotations !== extraction.source.quotations ||
      draft.demo !== extraction.source.demo);
  const currentReview = isCurrentReview(review, draft) && !sourceChanged;
  const findings = currentReview ? review.report.findings : [];
  const blocks = findings.filter((f) => f.severity === "block").length;
  async function extract() {
    const snapshot = current.current;
    setBusy("extract");
    setNotice("");
    try {
      const result = await api("extractions", {
        requirements,
        quotations,
        demo: draft.demo,
      });
      if (snapshot !== current.current) {
        setNotice("处理期间资料已修改，未覆盖你的编辑，请重新抽取。");
        return;
      }
      setWorkspace((w) => ({
        ...w,
        extraction: result,
        draft: result.draft,
        review: null,
      }));
      setConfirmed(false);
      setNotice(
        "已完成真实模型抽取。请对照每条原文核对清单，缺失字段须手动补齐；审批与票据仍待人工确认。",
      );
    } catch (error) {
      setNotice(error.message);
    } finally {
      setBusy("");
    }
  }
  async function audit() {
    let payload;
    try {
      payload = reviewPayload(draft, extraction?.id, confirmed);
    } catch (error) {
      setNotice(error.message);
      return;
    }
    const snapshot = current.current;
    setBusy("audit");
    setNotice("");
    try {
      const result = await api("campus/reviews", payload);
      if (snapshot !== current.current) {
        setNotice("预审期间清单已修改，请重新确认并预审。");
        return;
      }
      setWorkspace((w) => ({ ...w, draft: result.input, review: result }));
      setConfirmed(false);
    } catch (error) {
      setNotice(error.message);
    } finally {
      setBusy("");
    }
  }
  function reset() {
    change({
      requirements: requirementsExample,
      quotations: quotationsExample,
      draft: initialDraft(),
      extraction: null,
      review: null,
    });
    setNotice("已恢复合成示例；服务器中已保存的记录不会被删除。");
  }
  return (
    <div className="campus-app">
      <header className="campus-topbar">
        <a className="campus-brand" href="/campus">
          <span className="campus-brand-mark">S</span>
          <span>
            <strong>StablePay</strong>
            <small>生活好帮手 · 校园采购守门员</small>
          </span>
        </a>
        <div className="campus-topbar-center">
          <span className="campus-live-dot" />
          <span>支付宝「智能体涌现奖」参赛 MVP</span>
        </div>
        <button className="campus-reset" onClick={reset}>
          恢复合成示例
        </button>
      </header>
      <main className="campus-shell">
        <section className="campus-page-heading">
          <div>
            <div className="campus-eyebrow">
              从原文到预审 · 每个字段有据可查
            </div>
            <h1>
              先读懂采购资料，
              <br />
              再决定下一步。
            </h1>
            <p>
              AI
              整理活动需求和供应商报价；你确认事实，规则核对预算、规格、审批与票据。
            </p>
          </div>
          <div className="campus-flow">
            <span className="is-current">
              <i>1</i>读取原文
            </span>
            <b />
            <span>
              <i>2</i>人工确认
            </span>
            <b />
            <span>
              <i>3</i>预审导出
            </span>
          </div>
        </section>
        <div className="campus-disclosure">
          <span>ⓘ</span>
          <p>
            原文匹配只证明引用可回查，不证明材料真实。人工核验状态由使用者记录。默认不加载学校制度；可选参考包仅覆盖部分条款，不构成审批或报销保证。
          </p>
        </div>
        <div className="campus-workspace">
          <section className="campus-main-column">
            <article className="campus-panel">
              <div className="campus-panel-heading">
                <div>
                  <span className="campus-step-index">01</span>
                  <div>
                    <h2>粘贴活动需求与报价</h2>
                    <p>支持文本。图片请先转为文字；此版本未接入截图识别。</p>
                  </div>
                </div>
              </div>
              <div className="campus-source-inputs">
                <Field label="活动需求原文">
                  <textarea
                    rows="7"
                    maxLength={12000}
                    value={requirements}
                    onChange={(e) => change({ requirements: e.target.value })}
                  />
                </Field>
                <Field label="供应商报价原文">
                  <textarea
                    rows="9"
                    maxLength={12000}
                    value={quotations}
                    onChange={(e) => change({ quotations: e.target.value })}
                  />
                </Field>
                <label className="campus-confirm">
                  <input
                    type="checkbox"
                    checked={draft.demo}
                    onChange={(e) => changeDraft("demo", e.target.checked)}
                  />
                  这些是合成演示资料，不能作为真实采购凭证
                </label>
                <p className="campus-agent-hint">
                  点击后，两份文本将发送到已配置的模型服务。请先去除姓名、手机号等无关个人信息。
                </p>
                <div className="campus-ai-actions">
                  <button
                    className="campus-primary-button"
                    disabled={
                      Boolean(busy) ||
                      !status?.extraction_configured ||
                      !requirements.trim() ||
                      !quotations.trim()
                    }
                    onClick={extract}
                  >
                    {busy === "extract"
                      ? "AI 正在读取原文…"
                      : "用 AI 抽取采购清单"}
                  </button>
                  <span>
                    {status?.unavailable
                      ? "本机服务不可用，请用校园模式启动"
                      : status?.extraction_configured
                        ? "模型已配置 · 结果必须人工确认"
                        : "模型未配置 · 可手工填表预审"}
                  </span>
                </div>
              </div>
            </article>
            {notice && (
              <div className="campus-notice" role="status">
                {notice}
              </div>
            )}
            {sourceChanged && (
              <div className="campus-notice" role="alert">
                原文或演示标记已变化，请重新抽取，当前引用仍对应上一次原文。
              </div>
            )}
            {extraction && (
              <article className="campus-panel">
                <div className="campus-panel-heading">
                  <div>
                    <span className="campus-step-index">AI</span>
                    <div>
                      <h2>原文引用与待确认问题</h2>
                      <p>
                        {extraction.provider} · {extraction.model} ·{" "}
                        {extraction.fields.length} 条可定位字段
                      </p>
                    </div>
                  </div>
                </div>
                <div className="campus-citations">
                  {extraction.questions.map((q, index) => (
                    <p className="campus-question" key={index}>
                      待确认：{q}
                    </p>
                  ))}
                  <details className="campus-evidence-list">
                    <summary>
                      展开 {extraction.fields.length} 条字段引用
                    </summary>
                    {extraction.fields.map((field, index) => {
                      const source =
                        field.source === "requirements"
                          ? extraction.source.requirements
                          : extraction.source.quotations;
                      const excerpt = sourceExcerpt(source, field);
                      return (
                        <details className="campus-citation" key={index}>
                          <summary>
                            {field.item_index === undefined
                              ? ""
                              : "物资 " + (field.item_index + 1) + " · "}
                            {fieldNames[field.field] || field.field}：
                            {field.value}
                          </summary>
                          <blockquote>
                            {excerpt ? (
                              <>
                                {excerpt.before}
                                <mark>{excerpt.quote}</mark>
                                {excerpt.after}
                              </>
                            ) : (
                              "引用无法定位，请重新抽取。"
                            )}
                          </blockquote>
                          <small>
                            {field.source === "requirements"
                              ? "需求原文"
                              : "报价原文"}{" "}
                            · 字符 [{field.start_rune}, {field.end_rune}) ·
                            SHA256 {field.source_hash.slice(0, 12)}…
                          </small>
                        </details>
                      );
                    })}
                  </details>
                </div>
              </article>
            )}
            <article className="campus-panel">
              <div className="campus-panel-heading">
                <div>
                  <span className="campus-step-index">02</span>
                  <div>
                    <h2>核对活动与经费</h2>
                    <p>模型不会替你确认审批，也不会自动核验文件真实性。</p>
                  </div>
                </div>
              </div>
              <div className="campus-field-grid">
                <Field label="活动名称">
                  <input
                    value={draft.activity}
                    onChange={(e) => changeDraft("activity", e.target.value)}
                  />
                </Field>
                <Field label="学校 / 单位" hint="只作记录，不会自动匹配校规。">
                  <input
                    value={draft.school}
                    onChange={(e) => changeDraft("school", e.target.value)}
                  />
                </Field>
                <Field label="活动预算（元）">
                  <input
                    type="number"
                    min="0"
                    step="0.01"
                    value={draft.budget}
                    onChange={(e) => changeDraft("budget", e.target.value)}
                  />
                </Field>
                <Field label="审批状态（使用者声明）">
                  <select
                    value={draft.approved ? "yes" : "no"}
                    onChange={(e) =>
                      changeDraft("approved", e.target.value === "yes")
                    }
                  >
                    <option value="no">尚未完成审批</option>
                    <option value="yes">声明已取得审批，仍需核验</option>
                  </select>
                </Field>
                <Field label="预算依据">
                  <input
                    value={draft.budget_evidence}
                    onChange={(e) =>
                      changeDraft("budget_evidence", e.target.value)
                    }
                  />
                </Field>
                <Field
                  label="学校制度参考包（可选）"
                  hint="不会根据学校名称自动套用。"
                >
                  <select
                    value={draft.policy_id || ""}
                    onChange={(e) => {
                      setWorkspace((w) => ({
                        ...w,
                        draft: {
                          ...w.draft,
                          policy_id: e.target.value,
                          policy_scope_confirmed: false,
                        },
                      }));
                      setConfirmed(false);
                    }}
                  >
                    <option value="">不加载学校制度，仅做通用预审</option>
                    {policies.map((p) => (
                      <option key={p.id} value={p.id}>
                        {p.school} · {p.version}
                      </option>
                    ))}
                  </select>
                </Field>
              </div>
              {draft.policy_id &&
                policies
                  .filter((p) => p.id === draft.policy_id)
                  .map((p) => (
                    <div className="campus-policy" key={p.id}>
                      <strong>{p.title}</strong>
                      <p>{p.scope}</p>
                      <a href={p.url} target="_blank" rel="noreferrer">
                        查看学校官网原文 ↗
                      </a>
                      <label className="campus-confirm">
                        <input
                          type="checkbox"
                          checked={Boolean(draft.policy_scope_confirmed)}
                          onChange={(e) =>
                            changeDraft(
                              "policy_scope_confirmed",
                              e.target.checked,
                            )
                          }
                        />
                        我已确认学校、社团类型和资金来源符合上述范围
                      </label>
                    </div>
                  ))}
              <div className="campus-proofs">
                <Proof
                  label="预算依据"
                  value={draft.budget_proof}
                  onChange={(v) => changeDraft("budget_proof", v)}
                />
                <Proof
                  label="负责人审批"
                  value={draft.approval_proof}
                  onChange={(v) => changeDraft("approval_proof", v)}
                />
              </div>
            </article>
            <article className="campus-panel campus-items-panel">
              <div className="campus-panel-heading">
                <div>
                  <span className="campus-step-index">03</span>
                  <div>
                    <h2>确认采购明细</h2>
                    <p>
                      支持明确标注的 mm、cm、m 等价长度换算；未知字段请补齐。
                    </p>
                  </div>
                </div>
                <span className="campus-item-count">
                  {draft.items.length} 项
                </span>
              </div>
              <div className="campus-items-list">
                {draft.items.map((item, index) => (
                  <div className="campus-item-card" key={index}>
                    <div className="campus-item-title-row">
                      <span className="campus-item-number">
                        {String(index + 1).padStart(2, "0")}
                      </span>
                      <input
                        aria-label={"物资 " + (index + 1) + " 名称"}
                        className="campus-item-name"
                        value={item.name}
                        onChange={(e) =>
                          changeItem(index, "name", e.target.value)
                        }
                      />
                      <button
                        className="campus-remove-button"
                        onClick={() =>
                          changeDraft(
                            "items",
                            draft.items.filter((_, i) => i !== index),
                          )
                        }
                      >
                        移除
                      </button>
                    </div>
                    <div className="campus-item-fields">
                      <Field label={"物资 " + (index + 1) + " 需求规格"}>
                        <input
                          value={item.requested_spec}
                          onChange={(e) =>
                            changeItem(index, "requested_spec", e.target.value)
                          }
                        />
                      </Field>
                      <Field label={"物资 " + (index + 1) + " 报价规格"}>
                        <input
                          value={item.quoted_spec}
                          onChange={(e) =>
                            changeItem(index, "quoted_spec", e.target.value)
                          }
                        />
                      </Field>
                      <Field label={"物资 " + (index + 1) + " 数量"}>
                        <input
                          type="number"
                          min="1"
                          step="1"
                          value={item.quantity || ""}
                          onChange={(e) =>
                            changeItem(index, "quantity", e.target.value)
                          }
                        />
                      </Field>
                      <Field label={"物资 " + (index + 1) + " 单价（元）"}>
                        <input
                          type="number"
                          min="0.01"
                          step="0.01"
                          value={item.unit_price}
                          onChange={(e) =>
                            changeItem(index, "unit_price", e.target.value)
                          }
                        />
                      </Field>
                      <Field label={"物资 " + (index + 1) + " 报价来源"}>
                        <input
                          value={item.quote_source}
                          onChange={(e) =>
                            changeItem(index, "quote_source", e.target.value)
                          }
                        />
                      </Field>
                    </div>
                    <div className="campus-item-footer">
                      <Field label={"物资 " + (index + 1) + " 票据声明"}>
                        <select
                          value={item.invoice_confirmed ? "yes" : "no"}
                          onChange={(e) =>
                            changeItem(
                              index,
                              "invoice_confirmed",
                              e.target.value === "yes",
                            )
                          }
                        >
                          <option value="no">尚未确认可开票</option>
                          <option value="yes">
                            声明供应商可开票，仍需核验
                          </option>
                        </select>
                      </Field>
                      <div className="campus-line-total">
                        <span>小计</span>
                        <strong>{money(previewTotal([item]))}</strong>
                      </div>
                    </div>
                    <Proof
                      label={"物资 " + (index + 1) + " 报价"}
                      value={item.quote_proof}
                      onChange={(v) => changeItem(index, "quote_proof", v)}
                    />
                    <Proof
                      label={"物资 " + (index + 1) + " 票据"}
                      value={item.invoice_proof}
                      onChange={(v) => changeItem(index, "invoice_proof", v)}
                    />
                  </div>
                ))}
              </div>
              <button
                className="campus-add-item"
                disabled={draft.items.length >= 50}
                onClick={() =>
                  changeDraft("items", [...draft.items, newItem()])
                }
              >
                ＋ 添加采购项
              </button>
              <div className="campus-total-row">
                <span>当前方案合计</span>
                <strong>{money(total)}</strong>
              </div>
            </article>
            <div className="campus-confirm-panel">
              <label className="campus-confirm">
                <input
                  type="checkbox"
                  checked={confirmed}
                  disabled={Boolean(sourceChanged)}
                  onChange={(e) => setConfirmed(e.target.checked)}
                />
                我已对照原文逐项核对当前清单；这次确认不会代替学校审批。
              </label>
              <button
                className="campus-primary-button"
                disabled={!confirmed || Boolean(busy) || Boolean(sourceChanged)}
                onClick={audit}
              >
                {busy === "audit" ? "正在预审…" : "确认清单并运行预审"}
              </button>
            </div>
          </section>
          <aside className="campus-side-column">
            <article className="campus-panel campus-result-panel">
              <div className="campus-result-topline">
                <span>采购预审结果</span>
                <span className="campus-demo-tag">确定性规则</span>
              </div>
              {!currentReview ? (
                <div className="campus-result-empty">
                  <div className="campus-empty-orbit">
                    <span>✓</span>
                  </div>
                  <h2>
                    {review
                      ? "清单已修改，需要重新预审"
                      : "确认事实，再查看风险"}
                  </h2>
                  <p>
                    先用 AI
                    整理原文或手动填写清单，再核对并明确确认。修改后的旧报告不能继续导出。
                  </p>
                </div>
              ) : (
                <div className="campus-result-content">
                  <div
                    className={
                      "campus-status-banner " +
                      (blocks ? "block" : findings.length ? "review" : "ready")
                    }
                  >
                    <span className="campus-status-icon">
                      {findings.length ? "!" : "✓"}
                    </span>
                    <div>
                      <strong>
                        {blocks
                          ? "暂缓采购确认"
                          : findings.length
                            ? "需要补充核实"
                            : "当前规则未发现问题"}
                      </strong>
                      <span>
                        {blocks} 项需处理 · {findings.length - blocks} 项待核实
                      </span>
                    </div>
                  </div>
                  <div className="campus-budget-card">
                    <div>
                      <span>采购合计</span>
                      <strong>{money(review.report.total_minor)}</strong>
                    </div>
                    <div>
                      <span>填写预算</span>
                      <strong>
                        {review.report.budget_minor > 0
                          ? money(review.report.budget_minor)
                          : "待填写"}
                      </strong>
                    </div>
                    <div
                      className={
                        "campus-budget-delta " +
                        (review.report.budget_minor > 0 &&
                        review.report.total_minor > review.report.budget_minor
                          ? "over"
                          : "")
                      }
                    >
                      <span>
                        {review.report.budget_minor <= 0
                          ? "预算待填写"
                          : review.report.total_minor >
                              review.report.budget_minor
                            ? "超出预算"
                            : "预算余量"}
                      </span>
                      <strong>
                        {review.report.budget_minor > 0
                          ? money(
                              Math.abs(
                                review.report.total_minor -
                                  review.report.budget_minor,
                              ),
                            )
                          : "待填写"}
                      </strong>
                    </div>
                  </div>
                  <div className="campus-risk-list">
                    {findings.map((finding, index) => (
                      <article
                        className={"campus-risk-card " + finding.severity}
                        key={index}
                      >
                        <div className="campus-risk-title">
                          <span>!</span>
                          <strong>{finding.title}</strong>
                        </div>
                        <p>{finding.evidence}</p>
                        <div className="campus-risk-action">
                          <b>建议</b>
                          {finding.action}
                        </div>
                      </article>
                    ))}
                  </div>
                  {review.changes_after_extraction.length > 0 && (
                    <p className="campus-agent-hint">
                      已记录 {review.changes_after_extraction.length}{" "}
                      个抽取后修改字段。原始引用保留在导出报告中。
                    </p>
                  )}
                  <p className="campus-agent-hint">
                    {review.report.rules_version} ·{" "}
                    {review.input_hash.slice(0, 12)}…
                  </p>
                  <a
                    className="campus-export-button"
                    href={"/api/mvp/campus/reviews/" + review.id + "/export"}
                    download="校园采购预审.md"
                  >
                    下载含原文引用的预审报告 ↓
                  </a>
                </div>
              )}
            </article>
            <article className="campus-panel campus-scope-panel">
              <div className="campus-scope-icon">i</div>
              <div>
                <h3>能力与资料边界</h3>
                <p>
                  AI
                  抽取文本，原文引用由服务端定位；规则检查预算、长度规格和证据状态。AI
                  不授权审批，也不执行付款。
                </p>
                <p className="campus-scope-footnote">
                  默认不加载校规；可选一所学校的官网制度参考，仅提示部分待办。未验证材料真实性。公网访问与真实学生试用待完成。
                </p>
              </div>
            </article>
            <div className="campus-trust-note">
              <span>▣</span>
              <p>
                {storageFailed
                  ? "浏览器未能保存草稿；关闭页面前请完成预审并导出。"
                  : "草稿自动保存在此浏览器，刷新可恢复。原文和预审快照保存在本机服务中；清除浏览器数据会失去会话访问权限。"}
              </p>
            </div>
          </aside>
        </div>
        <footer className="campus-footer">
          <span>StablePay · 校园采购守门员</span>
          <span>预审辅助，不替代学校审批或财务审核</span>
        </footer>
      </main>
    </div>
  );
}
