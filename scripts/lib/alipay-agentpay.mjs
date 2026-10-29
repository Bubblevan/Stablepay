// Official PC Agent Pay tools run on the buyer's Agent host. The server never
// shells out, holds a wallet password, or trusts this tool output as payment.
import { execFile } from 'node:child_process'
import { promisify } from 'node:util'
import { createHash } from 'node:crypto'
import { mkdir, readFile, writeFile, access } from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import { resolveAgentPaySession, orderPaymentContext, validatePaymentLink, signalArguments, submitArguments } from './alipay-agentpay-protocol.mjs'

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

async function tool(command, args, execute = exec) {
  // All parameters are argv elements; merchant text and signed URLs never
  // become a shell command. Prevent inherited model/payment credentials from
  // leaking into this independent official payment tool.
  const env = Object.fromEntries(Object.entries(process.env).filter(([name]) => !/^(LLM_|ALIPAY_(APP|SELLER|PRIVATE|PUBLIC|KEY)|FIAT_INTERNAL_TOKEN|SOLANA_)/i.test(name)))
  return execute(await executable(), [command, ...args, '--json'], { shell: false, windowsHide: true, timeout: 60000, maxBuffer: 1024 * 1024, env })
}

function decoded(stdout) {
  const value = JSON.parse(stdout.trim())
  if (!value || typeof value !== 'object' || Array.isArray(value) || value.success === false) throw new Error('Official Alipay tool did not return a successful JSON response')
  return value
}

function rejectionSummary(raw) {
  try {
    const data=JSON.parse(raw)
    if (data.success===false && /^[A-Z0-9_]{1,80}$/.test(data.errorCode || '')) {
      const message=String(data.errorMsg || data.errorMessage || '').replace(/https?:\/\/\S+/g,'[redacted-url]').slice(0,160)
      return `${data.errorCode}${message ? ` (${message})` : ''}`
    }
  } catch {}
  return ''
}

export async function inspectAgentPayEnvironment({ sessionDir }) {
  const version=await exec(await executable(),['--version'],{shell:false,windowsHide:true,timeout:10000,maxBuffer:65536})
  const response=await tool('check-wallet',[])
  const wallet=JSON.parse(response.stdout.trim())
  await mkdir(sessionDir,{recursive:true,mode:0o700})
  await writeFile(path.join(sessionDir,'wallet-check-private.json'),response.stdout,{mode:0o600})
  // Only expose readiness, never wallet identifiers, credentials or account data.
  return { cli_version:version.stdout.trim(), wallet_status:wallet.status || 'unknown', wallet_never_created:wallet.neverCreated===true, standalone_diagnostic:true, payment_precondition:false }
}

export async function runAgentPay({ command, purchaseId, call, sessionDir, confirm, recoverSignal = false, environment = process.env, execute = exec }) {
  if (!['agentpay-prepare', 'agentpay-submit'].includes(command)) throw new Error('Unsupported Agent Pay command')
  const session = resolveAgentPaySession(environment)
  if (command === 'agentpay-submit' && !confirm) throw new Error('agentpay-submit requires --confirm after the user authorizes this exact merchant, service and amount')
  const dir = path.join(sessionDir, 'agentpay')
  await mkdir(dir, { recursive: true, mode: 0o700 })
  const stateFile = path.join(dir, `${purchaseId}.json`)
  const purchase = await call(`purchases/${purchaseId}`)
  if (purchase.quote.mode !== 'alipay_sandbox') throw new Error('This adapter currently allows only Alipay sandbox orders')
  if (purchase.status !== 'AWAITING_PAYMENT') throw new Error('Order is no longer awaiting payment; read its receipt instead')
  // A sandbox can permit Node itself while denying its native child process.
  // Detect that before taking a durable one-shot tool claim.
  await execute(await executable(), ['--version'], { shell: false, windowsHide: true, timeout: 10000, maxBuffer: 65536 })

  if (command === 'agentpay-prepare') {
    const handoff = await call(`purchases/${purchaseId}/agentpay-handoff`, {})
    if (handoff.purchase_id !== purchaseId || handoff.quote_hash !== purchase.quote.hash || handoff.approval_ref !== purchase.approval_ref || handoff.payment_mode !== 'alipay_sandbox' || handoff.harness_policy.decision !== 'ALLOW') throw new Error('Server handoff does not match confirmed quote')
    const context = orderPaymentContext(purchase)
    if (handoff.amount !== context.amount || handoff.merchant_info !== context.merchantInfo) throw new Error('Server handoff merchant or amount differs from the confirmed order')
    const cashier = new URL(handoff.cashier_url)
    if (cashier.origin !== 'https://openapi-sandbox.dl.alipaydev.com' || cashier.pathname !== '/gateway.do' || cashier.searchParams.get('method') !== 'alipay.trade.page.pay' || cashier.username || cashier.password) throw new Error('Unexpected sandbox cashier URL')
    const attempt = await call(`purchases/${purchaseId}/agentpay-claim`, { stage: 'signal', recover_signal: recoverSignal })
    let output = '', linkDigest = '', saved = false
    try {
      const response = await tool('trigger-payment-signal', signalArguments({ cashierURL: handoff.cashier_url, context, session }), execute)
      output = response.stdout
      const result = decoded(output)
      // paymentLink is the field in Alipay's official merchant Skill example.
      // It is opaque: never display, open, normalize or re-encode it.
      validatePaymentLink(result.paymentLink)
      linkDigest = sha(result.paymentLink)
      await writeFile(stateFile, JSON.stringify({ purchase_id: purchaseId, agent_session_id: session.id, agent_session_source: session.source, quote_hash: handoff.quote_hash, approval_ref: handoff.approval_ref, merchant_info: handoff.merchant_info, amount: handoff.amount, payment_link: result.paymentLink, link_digest: linkDigest, stage: 'SIGNAL_READY' }), { mode: 0o600 })
      saved = true
      await writeFile(path.join(dir, `${attempt.id}.json`), output, { mode: 0o600 })
      await call(`purchases/${purchaseId}/agentpay-result`, { attempt_id: attempt.id, status: 'CLIENT_REPORTED_SUCCESS', output_digest: sha(output), link_digest: linkDigest })
      return { purchase_id: purchaseId, stage: 'SIGNAL_READY', payment_mode: 'alipay_sandbox', paid: false, next: 'After explicit human approval, run agentpay-submit with --confirm. Sandbox compatibility of the official payment host still needs live validation.' }
    } catch (error) {
      const raw = output || String(error.stdout || '') || JSON.stringify({ error: 'Official tool failed or response was not accepted', code: error.code ?? null, syscall: error.syscall ?? null, stderr: String(error.stderr || ''), state_saved: saved })
      await writeFile(path.join(dir, `${attempt.id}.json`), raw, { mode: 0o600 })
      await call(`purchases/${purchaseId}/agentpay-result`, { attempt_id: attempt.id, status: 'UNKNOWN', output_digest: sha(raw), link_digest: linkDigest }).catch(() => {})
      throw new Error('Alipay signal outcome is uncertain. Inspect the private tool output and order; do not automatically rerun this stage.')
    }
  }

  // Official payment Skill forbids a check-wallet preflight. submit-payment
  // handles wallet readiness; doctor remains an explicit standalone diagnostic.
  const state = JSON.parse(await readFile(stateFile, 'utf8'))
  const args = submitArguments({ state, purchase, session, digest:sha })
  const attempt = await call(`purchases/${purchaseId}/agentpay-claim`, { stage: 'submit', link_digest: state.link_digest })
  state.stage = 'SUBMIT_REQUESTED'
  await writeFile(stateFile, JSON.stringify(state), { mode: 0o600 })
  let output = ''
  try {
    const response = await tool('submit-payment', args, execute)
    output = response.stdout
    decoded(output)
    await writeFile(path.join(dir, `${attempt.id}.json`), output, { mode: 0o600 })
    await call(`purchases/${purchaseId}/agentpay-result`, { attempt_id: attempt.id, status: 'CLIENT_REPORTED_SUCCESS', output_digest: sha(output), link_digest: state.link_digest })
    state.stage = 'SUBMITTED'
    await writeFile(stateFile, JSON.stringify(state), { mode: 0o600 })
    return { purchase_id: purchaseId, stage: 'SUBMITTED', paid: false, next: 'Complete the official user payment flow, then run reconcile and receipt. Only the server-verified order can prove payment.', private_tool_output: path.join(dir, `${attempt.id}.json`) }
  } catch (error) {
    const raw = output || String(error.stdout || '') || JSON.stringify({ error: 'Official payment submission outcome uncertain', code: error.code ?? null, syscall: error.syscall ?? null, stderr: String(error.stderr || '') })
    await writeFile(path.join(dir, `${attempt.id}.json`), raw, { mode: 0o600 })
    await call(`purchases/${purchaseId}/agentpay-result`, { attempt_id: attempt.id, status: 'UNKNOWN', output_digest: sha(raw), link_digest: state.link_digest }).catch(() => {})
    const rejected=rejectionSummary(raw)
    throw new Error(`${rejected ? `Official payment tool rejected the request: ${rejected}.` : 'Payment tool outcome is uncertain.'} Reconcile this order; do not submit again or create a replacement payment automatically.`)
  }
}
