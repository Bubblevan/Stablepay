import { useEffect, useMemo, useRef, useState } from 'react'

const initialItems = [
  {
    id: 1,
    name: '活动横幅',
    requestedSpec: '3m × 0.8m',
    quotedSpec: '3m × 0.8m',
    quoteSource: '演示报价单（非真实来源）',
    quantity: '1',
    unitPrice: '180',
    invoiceStatus: 'confirmed',
  },
  {
    id: 2,
    name: '宣传展架',
    requestedSpec: '80cm × 180cm',
    quotedSpec: '60cm × 160cm',
    quoteSource: '演示报价单（非真实来源）',
    quantity: '2',
    unitPrice: '68',
    invoiceStatus: 'confirmed',
  },
  {
    id: 3,
    name: '饮用水',
    requestedSpec: '24 瓶 / 箱',
    quotedSpec: '24 瓶 / 箱',
    quoteSource: '演示报价单（非真实来源）',
    quantity: '4',
    unitPrice: '35',
    invoiceStatus: 'unknown',
  },
  {
    id: 4,
    name: '签到桌牌',
    requestedSpec: '亚克力，A5',
    quotedSpec: '亚克力，A5',
    quoteSource: '演示报价单（非真实来源）',
    quantity: '4',
    unitPrice: '20',
    invoiceStatus: 'confirmed',
  },
]

const currency = new Intl.NumberFormat('zh-CN', {
  style: 'currency',
  currency: 'CNY',
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
})

function toCents(value) {
  const raw = String(value).trim()
  if (!/^\d{1,9}(?:\.\d{1,2})?$/.test(raw)) return 0
  const [whole, fraction = ''] = raw.split('.')
  return Number(whole) * 100 + Number(fraction.padEnd(2, '0'))
}

function money(cents) {
  return currency.format(cents / 100)
}

function normalizeSpec(value) {
  return value.trim().toLocaleLowerCase('zh-CN').replace(/[\s×*]/g, '')
}

function lineAmount(item) {
  const quantity = Number(item.quantity)
  const count = Number.isSafeInteger(quantity) && quantity > 0 ? quantity : 0
  const priceCents = toCents(item.unitPrice)
  return priceCents > 0 ? Math.round(count * priceCents) : 0
}

function runChecks({ budgetCents, budgetEvidence, approvalStatus, items }) {
  const totalCents = items.reduce((total, item) => total + lineAmount(item), 0)
  const risks = []

  if (budgetCents <= 0) {
    risks.push({
      key: 'budget-missing',
      severity: 'review',
      title: '尚未填写批准预算',
      detail: '补充活动可用额度后，才能判断当前方案是否超支。',
      action: '填写本次活动已确认的预算金额。',
    })
  } else if (totalCents > budgetCents) {
    risks.push({
      key: 'over-budget',
      severity: 'block',
      title: `方案超预算 ${money(totalCents - budgetCents)}`,
      detail: `采购明细合计 ${money(totalCents)}，比填写的预算 ${money(budgetCents)} 多 ${money(totalCents - budgetCents)}。`,
      action: `至少压减 ${money(totalCents - budgetCents)}，或先完成追加预算审批并更新额度。`,
    })
  }

  if (approvalStatus !== 'approved') {
    risks.push({
      key: 'approval-pending',
      severity: 'block',
      title: '负责人审批状态待确认',
      detail: '当前方案标记为尚未完成负责人审批。',
      action: '按本校流程取得所需审批后，再进入采购确认。',
    })
  } else if (!budgetEvidence.trim()) {
    risks.push({
      key: 'budget-evidence-missing',
      severity: 'review',
      title: '预算审批依据尚未留档',
      detail: '当前状态由使用者填写为已确认，但没有记录对应审批文件或记录位置，无法独立核实。',
      action: '补充预算批复文件名、链接或记录编号，并保留可核查材料。',
    })
  }

  if (items.length === 0) {
    risks.push({
      key: 'items-empty',
      severity: 'review',
      title: '采购清单为空',
      detail: '添加至少一项物资和对应报价后，才能执行清单核验。',
      action: '补充物资名称、数量、报价与需求规格。',
    })
  }

  items.forEach((item) => {
    const label = item.name.trim() || '未命名物资'
    const quantity = Number(item.quantity)
    const priceCents = toCents(item.unitPrice)

    if (!item.name.trim() || !Number.isSafeInteger(quantity) || quantity <= 0 || priceCents <= 0) {
      risks.push({
        key: `incomplete-${item.id}`,
        severity: 'review',
        title: `${label}的信息不完整`,
        detail: '需要有效的物资名称、正数数量和单价，才能核算明细金额。',
        action: '补齐缺失字段后重新预审。',
      })
    }

    if (!(item.quoteSource || '').trim()) {
      risks.push({
        key: `quote-source-${item.id}`,
        severity: 'review',
        title: `${label}尚未记录报价来源`,
        detail: '当前没有供应商名称、报价文件名或商品链接，无法回查该单价。',
        action: '填写供应商名称或报价文件 / 链接，并与原始报价核对。',
      })
    }

    const requiredSpec = normalizeSpec(item.requestedSpec)
    const quotedSpec = normalizeSpec(item.quotedSpec)
    if (!requiredSpec || !quotedSpec) {
      risks.push({
        key: `spec-missing-${item.id}`,
        severity: 'review',
        title: `${label}的规格信息待补充`,
        detail: '缺少需求规格或供应商报价规格，暂时无法确认是否为同一规格。',
        action: '对照活动需求与供应商原始报价补齐规格。',
      })
    } else if (requiredSpec !== quotedSpec) {
      risks.push({
        key: `spec-mismatch-${item.id}`,
        severity: 'review',
        title: `${label}的报价规格与需求不一致`,
        detail: `需求规格为“${item.requestedSpec}”，报价规格为“${item.quotedSpec}”。当前不能按同规格商品确认。`,
        action: '请供应商按需求规格重新报价，或由活动负责人确认是否接受替代规格。',
      })
    }

    if (item.invoiceStatus !== 'confirmed') {
      risks.push({
        key: `invoice-${item.id}`,
        severity: 'review',
        title: `${label}的票据条件尚未确认`,
        detail: '当前记录中没有供应商可按本次经费要求提供票据的确认。',
        action: '购买前向供应商确认票据类型、抬头及明细要求，并按本校制度留存证明。',
      })
    }
  })

  const blockingCount = risks.filter((risk) => risk.severity === 'block').length
  const reviewCount = risks.filter((risk) => risk.severity === 'review').length
  const status = blockingCount > 0
    ? { label: '暂缓采购确认', tone: 'block' }
    : reviewCount > 0
      ? { label: '需要补充核实', tone: 'review' }
      : { label: '信息齐备，可提交负责人确认', tone: 'ready' }

  return { totalCents, risks, status, blockingCount, reviewCount }
}

function makeReport({ eventName, schoolName, budgetCents, budgetEvidence, approvalStatus, items, audit }) {
  const statusText = approvalStatus === 'approved' ? '负责人已确认（由使用者填写）' : '尚未确认'
  const riskLines = audit.risks.length
    ? audit.risks.map((risk) => `- **${risk.title}**：${risk.detail}\n  - 建议：${risk.action}`).join('\n')
    : '- 当前输入未触发演示检查项。仍需按适用的学校制度和原始报价人工确认。'

  return [
    '# 校园活动采购预审清单',
    '',
    `- 活动：${eventName || '未填写'}`,
    `- 学校 / 单位：${schoolName || '未填写'}`,
    `- 预算：${budgetCents > 0 ? money(budgetCents) : '未填写'}`,
    `- 预算依据：${budgetEvidence || '未记录'}`,
    `- 负责人审批状态：${statusText}`,
    `- 明细合计：${money(audit.totalCents)}`,
    `- 预审状态：${audit.status.label}`,
    '',
    '## 采购明细',
    ...items.map((item) => `- ${item.name || '未命名'}：${item.quantity || 0} × ${money(toCents(item.unitPrice))}；需求规格 ${item.requestedSpec || '待补'}；报价规格 ${item.quotedSpec || '待补'}；报价来源 ${item.quoteSource || '未记录'}`),
    '',
    '## 风险与建议',
    riskLines,
    '',
    '> 本清单根据填写资料执行确定性检查，不是学校审批意见或报销保证。请以适用学校的现行制度、审批记录和供应商原始凭证为准。',
  ].join('\n')
}

function Field({ label, hint, className = '', children }) {
  return (
    <label className={`campus-field ${className}`}>
      <span>{label}</span>
      {children}
      {hint && <small>{hint}</small>}
    </label>
  )
}

function MoneyValue({ cents, className = '' }) {
  return <span className={className}>{money(cents)}</span>
}

export default function CampusProcurementApp() {
  const [eventName, setEventName] = useState('社团迎新分享会')
  const [schoolName, setSchoolName] = useState('')
  const [budget, setBudget] = useState('500')
  const [budgetEvidence, setBudgetEvidence] = useState('演示预算记录（非真实审批材料）')
  const [approvalStatus, setApprovalStatus] = useState('approved')
  const [items, setItems] = useState(initialItems)
  const [hasAudited, setHasAudited] = useState(false)
  const [remoteAudit, setRemoteAudit] = useState(null)
  const [checking, setChecking] = useState(false)
  const [auditSource, setAuditSource] = useState('浏览器本地规则')
  const currentInput = useRef('')
  currentInput.current = JSON.stringify([eventName, schoolName, budget, budgetEvidence, approvalStatus, items])

  useEffect(() => {
    const previousTitle = document.title
    document.title = '校园活动采购守门员 · StablePay'
    return () => { document.title = previousTitle }
  }, [])

  const budgetCents = toCents(budget)
  const previewTotal = useMemo(() => items.reduce((total, item) => total + lineAmount(item), 0), [items])
  const localAudit = useMemo(
    () => runChecks({ budgetCents, budgetEvidence, approvalStatus, items }),
    [budgetCents, budgetEvidence, approvalStatus, items],
  )
  const audit = remoteAudit || localAudit

  const runAudit = async () => {
    const reviewedInput = currentInput.current
    setChecking(true)
    setRemoteAudit(null)
    setAuditSource('浏览器本地规则')
    try {
      const statusResponse = await fetch('/api/mvp/status', { signal: AbortSignal.timeout(5000) })
      const status = statusResponse.ok ? await statusResponse.json() : null
      if (status?.review_profile) {
        const response = await fetch('/api/mvp/reviews', {
          method: 'POST', headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'StablePay' },
          signal: AbortSignal.timeout(10000),
          body: JSON.stringify({ activity: eventName, school: schoolName, budget, budget_evidence: budgetEvidence, approved: approvalStatus === 'approved', items: items.map((item) => ({ name: item.name, requested_spec: item.requestedSpec, quoted_spec: item.quotedSpec, quantity: Number(item.quantity), unit_price: item.unitPrice, quote_source: item.quoteSource, invoice_confirmed: item.invoiceStatus === 'confirmed' })) }),
        })
        if (!response.ok) throw new Error('server review unavailable')
        const report = await response.json()
        if (reviewedInput !== currentInput.current) return
        const risks = report.findings.map((finding, index) => ({ key: `${finding.code}-${index}`, severity: finding.severity, title: finding.title, detail: finding.evidence, action: finding.action }))
        const blockingCount = risks.filter((risk) => risk.severity === 'block').length
        const reviewCount = risks.length - blockingCount
        setRemoteAudit({ totalCents: report.total_minor, risks, blockingCount, reviewCount, status: blockingCount ? { label: '暂缓采购确认', tone: 'block' } : reviewCount ? { label: '需要补充核实', tone: 'review' } : { label: '信息齐备，可提交负责人确认', tone: 'ready' } })
        setAuditSource(`共享服务端核验 · ${report.rules_version}`)
      }
    } catch {
      setAuditSource('浏览器本地规则（共享服务未执行）')
    } finally {
      setChecking(false)
      if (reviewedInput === currentInput.current) setHasAudited(true)
    }
  }

  const updateItem = (id, field, value) => {
    setItems((current) => current.map((item) => item.id === id ? { ...item, [field]: value } : item))
    setHasAudited(false)
  }

  const addItem = () => {
    setItems((current) => [...current, {
      id: Date.now(),
      name: '',
      requestedSpec: '',
      quotedSpec: '',
      quoteSource: '',
      quantity: '1',
      unitPrice: '',
      invoiceStatus: 'unknown',
    }])
    setHasAudited(false)
  }

  const removeItem = (id) => {
    setItems((current) => current.filter((item) => item.id !== id))
    setHasAudited(false)
  }

  const exportReport = () => {
    const content = makeReport({ eventName, schoolName, budgetCents, budgetEvidence, approvalStatus, items, audit })
    const blob = new Blob([content], { type: 'text/markdown;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    const safeName = (eventName || 'campus-event').replace(/[\\/:*?"<>|]/g, '-').slice(0, 40)
    link.href = url
    link.download = `采购预审-${safeName}.md`
    document.body.appendChild(link)
    link.click()
    link.remove()
    window.setTimeout(() => URL.revokeObjectURL(url), 1000)
  }

  return (
    <div className="campus-app">
      <header className="campus-topbar">
        <a className="campus-brand" href="/stablepay" aria-label="返回 StablePay">
          <span className="campus-brand-mark">S</span>
          <span><strong>StablePay</strong><small>生活好帮手 · 参赛 Demo</small></span>
        </a>
        <div className="campus-topbar-center">
          <span className="campus-live-dot" />
          <span>校园活动采购守门员</span>
        </div>
        <a className="campus-back-link" href="/stablepay">返回 StablePay ↗</a>
      </header>

      <main className="campus-shell">
        <section className="campus-page-heading">
          <div>
            <div className="campus-eyebrow">采购前检查 · 01</div>
            <h1>这份采购清单，<br className="campus-mobile-break" />现在可以买吗？</h1>
            <p>把活动预算、实际报价、规格和票据条件放在一起核对，在付款前发现需要处理的问题。</p>
          </div>
          <div className="campus-flow" aria-label="预审流程">
            <span className="is-current"><i>1</i>整理方案</span><b />
            <span><i>2</i>联合预审</span><b />
            <span><i>3</i>负责人确认</span>
          </div>
        </section>

        <div className="campus-disclosure">
          <span aria-hidden="true">ⓘ</span>
          <p><strong>演示规则范围：</strong>当前未绑定具体学校制度。系统只核对你填写的预算、需求规格、负责人状态和供应商票据答复，不代表学校正式审批或报销保证。</p>
        </div>

        <div className="campus-workspace">
          <section className="campus-main-column">
            <article className="campus-panel">
              <div className="campus-panel-heading">
                <div><span className="campus-step-index">01</span><div><h2>活动与经费</h2><p>先明确这笔钱属于哪场活动、可用额度是多少。</p></div></div>
              </div>
              <div className="campus-field-grid">
                <Field label="活动名称">
                  <input value={eventName} onChange={(event) => { setEventName(event.target.value); setHasAudited(false) }} placeholder="例如：学院迎新活动" />
                </Field>
                <Field label="学校 / 单位" hint="用于之后匹配适用制度；本 Demo 尚未加载校规。">
                  <input value={schoolName} onChange={(event) => { setSchoolName(event.target.value); setHasAudited(false) }} placeholder="填写学校或学院（选填）" />
                </Field>
                <Field label="已确认的活动预算" hint="请填写审批通过的可用金额。">
                  <div className="campus-money-input"><span>¥</span><input type="number" min="0" step="0.01" value={budget} onChange={(event) => { setBudget(event.target.value); setHasAudited(false) }} /></div>
                </Field>
                <Field label="负责人审批状态" hint="由你根据当前实际审批记录填写。">
                  <select value={approvalStatus} onChange={(event) => { setApprovalStatus(event.target.value); setHasAudited(false) }}>
                    <option value="approved">已取得负责人确认</option>
                    <option value="pending">尚未完成审批</option>
                  </select>
                </Field>
                <Field label="预算审批依据" hint="填写批复文件名、链接或记录编号；当前示例不是真实材料。">
                  <input value={budgetEvidence} onChange={(event) => { setBudgetEvidence(event.target.value); setHasAudited(false) }} placeholder="例如：预算批复.pdf / 审批记录编号" />
                </Field>
              </div>
            </article>

            <article className="campus-panel campus-items-panel">
              <div className="campus-panel-heading campus-items-heading">
                <div><span className="campus-step-index">02</span><div><h2>采购明细与报价</h2><p>对照活动需求与供应商提供的信息填写。</p></div></div>
                <span className="campus-item-count">{items.length} 项</span>
              </div>

              <div className="campus-items-list">
                {items.map((item, index) => (
                  <div className="campus-item-card" key={item.id}>
                    <div className="campus-item-title-row">
                      <span className="campus-item-number">{String(index + 1).padStart(2, '0')}</span>
                      <input aria-label={`物资 ${index + 1} 名称`} className="campus-item-name" value={item.name} onChange={(event) => updateItem(item.id, 'name', event.target.value)} placeholder="物资名称" />
                      <button type="button" className="campus-remove-button" onClick={() => removeItem(item.id)} aria-label={`删除${item.name || '此物资'}`}>移除</button>
                    </div>
                    <div className="campus-item-fields">
                      <Field label="需求规格"><input value={item.requestedSpec} onChange={(event) => updateItem(item.id, 'requestedSpec', event.target.value)} placeholder="活动需要的规格" /></Field>
                      <Field label="报价规格"><input value={item.quotedSpec} onChange={(event) => updateItem(item.id, 'quotedSpec', event.target.value)} placeholder="供应商实际报价规格" /></Field>
                      <Field label="数量"><input type="number" min="0" step="1" value={item.quantity} onChange={(event) => updateItem(item.id, 'quantity', event.target.value)} /></Field>
                      <Field label="单价（元）"><input type="number" min="0" step="0.01" value={item.unitPrice} onChange={(event) => updateItem(item.id, 'unitPrice', event.target.value)} placeholder="0.00" /></Field>
                      <Field className="campus-source-field" label="报价来源" hint="记录供应商、报价文件名或商品链接，方便回查。">
                        <input value={item.quoteSource} onChange={(event) => updateItem(item.id, 'quoteSource', event.target.value)} placeholder="供应商 / 报价文件 / 商品链接" />
                      </Field>
                    </div>
                    <div className="campus-item-footer">
                      <label className="campus-invoice-field"><span className="campus-invoice-icon">票</span><span>票据条件</span>
                        <select value={item.invoiceStatus} onChange={(event) => updateItem(item.id, 'invoiceStatus', event.target.value)}>
                          <option value="confirmed">供应商已确认可开具</option>
                          <option value="unknown">尚未向供应商确认</option>
                        </select>
                      </label>
                      <div className="campus-line-total"><span>小计</span><strong><MoneyValue cents={lineAmount(item)} /></strong></div>
                    </div>
                  </div>
                ))}
              </div>

              <button type="button" className="campus-add-item" onClick={addItem}><span>＋</span>添加采购项</button>
              <div className="campus-total-row"><span>当前方案合计</span><strong><MoneyValue cents={previewTotal} /></strong></div>
            </article>

            <div className="campus-submit-row">
              <div><strong>所有检查都基于当前填写内容</strong><span>修改报价或审批状态后，需要重新预审。</span></div>
              <button type="button" className="campus-primary-button" disabled={checking} onClick={runAudit}><span>⌕</span>{checking ? '正在核验…' : '开始采购预审'}</button>
            </div>
          </section>

          <aside className="campus-side-column">
            <article className="campus-panel campus-result-panel">
              <div className="campus-result-topline"><span>预审结果</span><span className="campus-demo-tag">{hasAudited ? auditSource : '规则演示'}</span></div>
              {!hasAudited ? (
                <div className="campus-result-empty">
                  <div className="campus-empty-orbit"><span>✓</span></div>
                  <h2>准备好检查这份方案</h2>
                  <p>当前明细合计 <strong><MoneyValue cents={previewTotal} /></strong>。点击“开始采购预审”，查看预算、规格、审批和票据条件的逐项核验结果。</p>
                  <div className="campus-check-preview"><span>◇</span>预算与明细合计 <b>待检查</b></div>
                  <div className="campus-check-preview"><span>◇</span>需求与报价规格 <b>待检查</b></div>
                  <div className="campus-check-preview"><span>◇</span>审批与票据准备 <b>待检查</b></div>
                </div>
              ) : (
                <div className="campus-result-content">
                  <div className={`campus-status-banner ${audit.status.tone}`}>
                    <span className="campus-status-icon">{audit.status.tone === 'ready' ? '✓' : '!'}</span>
                    <div><strong>{audit.status.label}</strong><span>{audit.blockingCount} 项需先处理 · {audit.reviewCount} 项待核实</span></div>
                  </div>

                  <div className="campus-budget-card">
                    <div><span>采购方案合计</span><strong><MoneyValue cents={audit.totalCents} /></strong></div>
                    <div><span>已填写预算</span><strong><MoneyValue cents={budgetCents} /></strong></div>
                    <div className={`campus-budget-delta ${audit.totalCents > budgetCents && budgetCents > 0 ? 'over' : ''}`}>
                      <span>{budgetCents <= 0 ? '预算待填写' : audit.totalCents > budgetCents ? '超出预算' : '预算余量'}</span>
                      <strong>{budgetCents <= 0 ? '待填写' : <MoneyValue cents={Math.abs(budgetCents - audit.totalCents)} />}</strong>
                    </div>
                  </div>

                  <div className="campus-risk-heading"><h3>需要处理</h3><span>{audit.risks.length} 项</span></div>
                  {audit.risks.length ? (
                    <div className="campus-risk-list">
                      {audit.risks.map((risk) => (
                        <article className={`campus-risk-card ${risk.severity}`} key={risk.key}>
                          <div className="campus-risk-title"><span>{risk.severity === 'block' ? '!' : '•'}</span><strong>{risk.title}</strong></div>
                          <p>{risk.detail}</p>
                          <div className="campus-risk-action"><b>建议</b>{risk.action}</div>
                        </article>
                      ))}
                    </div>
                  ) : (
                    <div className="campus-all-clear"><span>✓</span><div><strong>没有触发演示检查项</strong><p>仍需按适用学校制度和原始报价进行人工确认。</p></div></div>
                  )}

                  <div className="campus-next-step"><span>下一步</span><p>{audit.risks.length ? '逐项确认并修正方案，再重新运行预审。' : '请由活动负责人核对原始材料后确认采购方案。'}</p></div>
                  <button type="button" className="campus-export-button" onClick={exportReport}>下载预审清单 <span>↓</span></button>
                </div>
              )}
            </article>

            <article className="campus-panel campus-scope-panel">
              <div className="campus-scope-icon">i</div>
              <div><h3>这次检查了什么</h3><p>预算总额、逐项数量与单价、活动需求规格与报价规格、审批状态记录、供应商票据答复。</p><p className="campus-scope-footnote">报价来源与校规文本尚未接入；有信息缺失时，系统会提示人工确认。</p></div>
            </article>
            <div className="campus-trust-note"><span>🔒</span><p>当前数据只保存在本次页面状态中，刷新页面会恢复示例方案。</p></div>
          </aside>
        </div>

        <footer className="campus-footer"><span>StablePay · 校园活动采购守门员</span><span>预审辅助，不替代学校审批或财务审核</span></footer>
      </main>
    </div>
  )
}
