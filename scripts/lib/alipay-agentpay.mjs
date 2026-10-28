// Official PC Agent Pay tools run on the buyer's Agent host. The server never
// shells out, holds a wallet password, or trusts this tool output as payment.
import { execFile } from 'node:child_process'
import { promisify } from 'node:util'
import { createHash } from 'node:crypto'
import { mkdir, readFile, writeFile, access } from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'

const exec = promisify(execFile)
const sha = value => createHash('sha256').update(value).digest('hex')

async function executable() {
  if (process.env.ALIPAY_BOT_PATH) {
    if (!path.isAbsolute(process.env.ALIPAY_BOT_PATH)) throw new Error('ALIPAY_BOT_PATH must be absolute')
    return process.env.ALIPAY_BOT_PATH
  }
  if (process.platform === 'win32') {
    const installed = path.join(os.homedir(), '.openclaw-autoclaw', 'alipay-bot-cli', 'bin', 'alipay-bot.exe')
    try { await access(installed); return installed } catch {}
    return 'alipay-bot.exe'
  }
  return 'alipay-bot'
}

async function tool(command, args) {
  // All parameters are argv elements; merchant text and signed URLs never
  // become a shell command. Prevent inherited model/payment credentials from
  // leaking into this independent official payment tool.
  const env = Object.fromEntries(Object.entries(process.env).filter(([name]) => !/^(LLM_|ALIPAY_(APP|SELLER|PRIVATE|PUBLIC|KEY)|FIAT_INTERNAL_TOKEN|SOLANA_)/i.test(name)))
  return exec(await executable(), [command, ...args, '--json'], { shell: false, windowsHide: true, timeout: 60000, maxBuffer: 1024 * 1024, env })
}

function decoded(stdout) {
  const value = JSON.parse(stdout.trim())
  if (!value || typeof value !== 'object' || Array.isArray(value) || value.success === false) throw new Error('Official Alipay tool did not return a successful JSON response')
  return value
}

export async function runAgentPay({ command, purchaseId, call, sessionDir, confirm }) {
  const dir = path.join(sessionDir, 'agentpay')
  await mkdir(dir, { recursive: true, mode: 0o700 })
  const stateFile = path.join(dir, `${purchaseId}.json`)
  const purchase = await call(`purchases/${purchaseId}`)
  if (purchase.quote.mode !== 'alipay_sandbox') throw new Error('This adapter currently allows only Alipay sandbox orders')
  if (purchase.status !== 'AWAITING_PAYMENT') throw new Error('Order is no longer awaiting payment; read its receipt instead')

  if (command === 'agentpay-prepare') {
    const handoff = await call(`purchases/${purchaseId}/agentpay-handoff`, {})
    if (handoff.purchase_id !== purchaseId || handoff.quote_hash !== purchase.quote.hash || handoff.approval_ref !== purchase.approval_ref || handoff.payment_mode !== 'alipay_sandbox' || handoff.harness_policy.decision !== 'ALLOW') throw new Error('Server handoff does not match confirmed quote')
    const cashier = new URL(handoff.cashier_url)
    if (cashier.origin !== 'https://openapi-sandbox.dl.alipaydev.com' || cashier.pathname !== '/gateway.do' || cashier.searchParams.get('method') !== 'alipay.trade.page.pay' || cashier.username || cashier.password) throw new Error('Unexpected sandbox cashier URL')
    const attempt = await call(`purchases/${purchaseId}/agentpay-claim`, { stage: 'signal' })
    let output = '', linkDigest = '', saved = false
    try {
      const response = await tool('trigger-payment-signal', ['--payment-link', handoff.cashier_url, '--merchant-info', handoff.merchant_info, '--amount', handoff.amount, '--session-id', purchaseId])
      output = response.stdout
      const result = decoded(output)
      // paymentLink is the field in Alipay's official merchant Skill example.
      // It is opaque: never display, open, normalize or re-encode it.
      if (typeof result.paymentLink !== 'string' || !result.paymentLink.trim() || result.paymentLink.length > 32768 || /[\r\n\0]/.test(result.paymentLink)) throw new Error('Official tool response has no valid paymentLink')
      linkDigest = sha(result.paymentLink)
      await writeFile(stateFile, JSON.stringify({ purchase_id: purchaseId, quote_hash: handoff.quote_hash, approval_ref: handoff.approval_ref, merchant_info: handoff.merchant_info, amount: handoff.amount, payment_link: result.paymentLink, link_digest: linkDigest, stage: 'SIGNAL_READY' }), { mode: 0o600 })
      saved = true
      await writeFile(path.join(dir, `${attempt.id}.json`), output, { mode: 0o600 })
      await call(`purchases/${purchaseId}/agentpay-result`, { attempt_id: attempt.id, status: 'CLIENT_REPORTED_SUCCESS', output_digest: sha(output), link_digest: linkDigest })
      return { purchase_id: purchaseId, stage: 'SIGNAL_READY', payment_mode: 'alipay_sandbox', paid: false, next: 'After explicit human approval, run agentpay-submit with --confirm. Sandbox compatibility of the official payment host still needs live validation.' }
    } catch (error) {
      const raw = output || String(error.stdout || '')
      await writeFile(path.join(dir, `${attempt.id}.json`), raw || JSON.stringify({ error: 'Official tool failed or response was not accepted', state_saved: saved }), { mode: 0o600 })
      await call(`purchases/${purchaseId}/agentpay-result`, { attempt_id: attempt.id, status: 'UNKNOWN', output_digest: sha(raw), link_digest: linkDigest }).catch(() => {})
      throw new Error('Alipay signal outcome is uncertain. Inspect the private tool output and order; do not automatically rerun this stage.')
    }
  }

  if (!confirm) throw new Error('agentpay-submit requires --confirm after the user authorizes this exact merchant, service and amount')
  const state = JSON.parse(await readFile(stateFile, 'utf8'))
  const amount = `${Math.trunc(purchase.quote.product.amount_minor / 100)}.${String(purchase.quote.product.amount_minor % 100).padStart(2, '0')}`
  const merchantInfo = `${purchase.quote.product.merchant}，${purchase.quote.product.name}，人民币${amount}元（沙箱）`
  if (state.stage !== 'SIGNAL_READY' || state.purchase_id !== purchaseId || state.quote_hash !== purchase.quote.hash || state.approval_ref !== purchase.approval_ref || state.link_digest !== sha(state.payment_link) || state.amount !== amount || state.merchant_info !== merchantInfo) throw new Error('Stored payment handoff differs from the confirmed order')
  const attempt = await call(`purchases/${purchaseId}/agentpay-claim`, { stage: 'submit', link_digest: state.link_digest })
  state.stage = 'SUBMIT_REQUESTED'
  await writeFile(stateFile, JSON.stringify(state), { mode: 0o600 })
  let output = ''
  try {
    const response = await tool('submit-payment', ['--payment-link', state.payment_link, '--intent-summary', state.merchant_info, '--session-id', purchaseId])
    output = response.stdout
    decoded(output)
    await writeFile(path.join(dir, `${attempt.id}.json`), output, { mode: 0o600 })
    await call(`purchases/${purchaseId}/agentpay-result`, { attempt_id: attempt.id, status: 'CLIENT_REPORTED_SUCCESS', output_digest: sha(output), link_digest: state.link_digest })
    state.stage = 'SUBMITTED'
    await writeFile(stateFile, JSON.stringify(state), { mode: 0o600 })
    return { purchase_id: purchaseId, stage: 'SUBMITTED', paid: false, next: 'Complete the official user payment flow, then run reconcile and receipt. Only the server-verified order can prove payment.', private_tool_output: path.join(dir, `${attempt.id}.json`) }
  } catch (error) {
    const raw = output || String(error.stdout || '')
    await writeFile(path.join(dir, `${attempt.id}.json`), raw || JSON.stringify({ error: 'Official payment submission outcome uncertain' }), { mode: 0o600 })
    await call(`purchases/${purchaseId}/agentpay-result`, { attempt_id: attempt.id, status: 'UNKNOWN', output_digest: sha(raw), link_digest: state.link_digest }).catch(() => {})
    throw new Error('Payment tool outcome is uncertain. Reconcile this order; do not submit again or create a replacement payment automatically.')
  }
}
