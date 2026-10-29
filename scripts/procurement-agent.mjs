// Agent-facing HTTP client for the same Runtime boundary used by the Web UI.
// No Alipay credentials are loaded here. Explicit Agent Pay commands delegate
// to the official local alipay-bot; payment proof always comes from the server.
import { readFile, writeFile, mkdir } from 'node:fs/promises'
import { createHash } from 'node:crypto'
import { fileURLToPath } from 'node:url'
import path from 'node:path'
import { runAgentPay, inspectAgentPayEnvironment } from './lib/alipay-agentpay.mjs'

const [command, ...argv] = process.argv.slice(2)
const option = (name) => { const index = argv.indexOf(name); return index < 0 ? undefined : argv[index + 1] }
const base = new URL(option('--base') || 'http://127.0.0.1:8093')
const local = ['localhost', '127.0.0.1', '[::1]'].includes(base.hostname)
if (!['http:', 'https:'].includes(base.protocol) || base.username || base.password || base.pathname !== '/' || base.search || base.hash || (!local && (base.protocol !== 'https:' || !argv.includes('--allow-remote')))) throw new Error('Use loopback, or explicitly allow a trusted HTTPS server with --allow-remote')
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const sessionDir = path.join(root, '.local-run', 'procurement-agent')
const sessionFile = path.join(sessionDir, `${createHash('sha256').update(base.origin).digest('hex').slice(0, 16)}.json`)
let cookie = ''
try { cookie = JSON.parse(await readFile(sessionFile, 'utf8')).cookie || '' } catch (error) { if (error.code !== 'ENOENT') throw error }

async function call(endpoint, body, key) {
  const response = await fetch(new URL(`/api/mvp/${endpoint}`, base), { method: body === undefined ? 'GET' : 'POST', redirect: 'error', signal: AbortSignal.timeout(30000), headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'StablePay', ...(cookie ? { Cookie: cookie } : {}), ...(key ? { 'Idempotency-Key': key } : {}) }, ...(body === undefined ? {} : { body: JSON.stringify(body) }) })
  const incoming = response.headers.get('set-cookie')
  if (incoming) {
    cookie = incoming.split(';')[0]
    await mkdir(sessionDir, { recursive: true })
    await writeFile(sessionFile, JSON.stringify({ cookie }), { mode: 0o600 })
  }
  const data = await response.json()
  if (!response.ok) throw new Error(data.error || `HTTP ${response.status}`)
  return data
}

const help = `Usage: node scripts/procurement-agent.mjs <command> [options]
  catalog | environment | history
  agentpay-doctor (standalone diagnostic only; never a payment prerequisite)
  plan --file task.json
  quote --file quote-request.json
  approve --file approval.json --confirm --idempotency-key <stable-key>
  checkout | reconcile | status | receipt --purchase-id <id>
  agentpay-prepare --purchase-id <id> [--recover-signal]
  agentpay-submit --purchase-id <id> --confirm
  simulate --purchase-id <id> --confirm-local-simulation
Optional: --base http://127.0.0.1:8093
Approval requires explicit user confirmation of service, merchant and CNY amount.
Agent Pay requires a real UUID from host AIPAY_SESSION_ID or the current Codex session.
checkout returns a payment handoff only; it is never proof of payment.
Session credentials stay under ignored .local-run/procurement-agent.`
if (!command || command === 'help' || command === '--help') {
  console.log(help)
} else {
  let body
  if (['plan', 'quote', 'approve'].includes(command)) {
    const file = option('--file')
    if (!file) throw new Error('--file is required')
    body = JSON.parse(await readFile(file, 'utf8'))
  }
  const purchaseId = option('--purchase-id')
  if (['checkout', 'reconcile', 'status', 'receipt', 'simulate', 'agentpay-prepare', 'agentpay-submit'].includes(command) && !/^[a-f0-9]{32}$/.test(purchaseId || '')) throw new Error('A valid --purchase-id is required')
  let result
  switch (command) {
    case 'catalog': result = await call('catalog'); break
    case 'environment': result = await call('status'); break
    case 'history': result = await call('purchases'); break
    case 'agentpay-doctor': result=await inspectAgentPayEnvironment({sessionDir:path.join(sessionDir,createHash('sha256').update(base.origin).digest('hex').slice(0,16),'agentpay')}); break
    case 'plan': result = await call('plans', body); break
    case 'quote': result = await call('quotes', body); break
    case 'approve': {
      const key = option('--idempotency-key')
      if (!argv.includes('--confirm') || body.accepted !== true || !key) throw new Error('Approval requires accepted=true, --confirm and a stable --idempotency-key after human confirmation')
      result = await call('purchases', body, key); break
    }
    case 'checkout': result = await call(`purchases/${purchaseId}/checkout`, {}); break
    case 'reconcile': result = await call(`purchases/${purchaseId}/reconcile`, {}); break
    case 'status': result = await call(`purchases/${purchaseId}`); break
    case 'receipt': result = await call(`purchases/${purchaseId}/receipt`); break
    case 'agentpay-prepare':
    case 'agentpay-submit':
      result = await runAgentPay({ command, purchaseId, call, sessionDir: path.join(sessionDir, createHash('sha256').update(base.origin).digest('hex').slice(0, 16)), confirm: argv.includes('--confirm'), recoverSignal: argv.includes('--recover-signal') }); break
    case 'simulate':
      if (!argv.includes('--confirm-local-simulation')) throw new Error('Explicit --confirm-local-simulation is required')
      if ((await call('status')).payment_mode !== 'local') throw new Error('Simulation is unavailable for Alipay providers')
      result = await call(`purchases/${purchaseId}/simulate`, {}); break
    default: throw new Error(help)
  }
  console.log(JSON.stringify(result, null, 2))
}
