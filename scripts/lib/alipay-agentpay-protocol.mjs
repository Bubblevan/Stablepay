// Pure parameter rules from alipay/payment-skills. Never create a session ID.
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i

export function resolveAgentPaySession(env = process.env) {
  for (const source of ['AIPAY_SESSION_ID', 'CODEX_THREAD_ID', 'CODEX_SESSION_ID']) {
    if (env[source] === undefined) continue
    const id = env[source]
    if (typeof id !== 'string' || id.length !== 36 || !uuid.test(id) || /^0{8}-0{4}-0{4}-0{4}-0{12}$/.test(id)) {
      throw new Error(`${source} must contain the real current Agent session UUID; no payment may proceed`)
    }
    return { id, source }
  }
  throw new Error('No real Agent session UUID is available. Supply the host-provided AIPAY_SESSION_ID or run inside the current Codex session; do not invent one')
}

export function orderPaymentContext(purchase) {
  const product = purchase?.quote?.product
  if (!product || product.currency !== 'CNY' || !Number.isSafeInteger(product.amount_minor) || product.amount_minor <= 0) throw new Error('Expected a confirmed CNY quote in integer fen')
  for (const value of [product.name, product.merchant]) {
    if (typeof value !== 'string' || !value.trim() || value.length > 300 || /[\x00-\x1f\x7f]/.test(value)) throw new Error('Confirmed service and merchant must be unambiguous text')
  }
  const amount = `${Math.trunc(product.amount_minor / 100)}.${String(product.amount_minor % 100).padStart(2, '0')}`
  return {
    amount,
    merchantInfo: `${product.merchant}，${product.name}，人民币${amount}元（沙箱）`,
    intentSummary: `服务内容：${product.name}，支付金额：¥${amount}，支付对象：${product.merchant}`,
  }
}

export function validatePaymentLink(value) {
  if (typeof value !== 'string' || !value.trim() || value.length > 32768 || /[\r\n\0]/.test(value)) throw new Error('Official tool response has no valid paymentLink')
  return value // Opaque: never normalize, trim, open, or re-encode.
}

export function signalArguments({ cashierURL, context, session }) {
  return ['--payment-link', cashierURL, '--merchant-info', context.merchantInfo, '--amount', context.amount, '--session-id', session.id]
}

export function submitArguments({ state, purchase, session, digest }) {
  const context = orderPaymentContext(purchase) // Re-extract on this submit step.
  if (!state.agent_session_id) throw new Error('Legacy handoff has no real Agent session binding. Preserve it; do not migrate or resubmit the old payment')
  if (state.agent_session_id !== session.id) throw new Error('Agent session changed since payment preparation; no submit claim was taken')
  const link = validatePaymentLink(state.payment_link)
  if (state.stage !== 'SIGNAL_READY' || state.purchase_id !== purchase.id || state.quote_hash !== purchase.quote.hash || state.approval_ref !== purchase.approval_ref || state.link_digest !== digest(link) || state.amount !== context.amount || state.merchant_info !== context.merchantInfo) throw new Error('Stored payment handoff differs from the confirmed order')
  return ['--payment-link', link, '--intent-summary', context.intentSummary, '--session-id', session.id]
}
