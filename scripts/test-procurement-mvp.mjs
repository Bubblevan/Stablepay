// Real HTTP smoke test across the running Runtime and payment-service processes.
// Refuses all non-local providers: this script cannot initiate an Alipay payment.
import assert from 'node:assert/strict'
import { randomUUID } from 'node:crypto'
import { writeFile } from 'node:fs/promises'
const base = process.argv[2] || 'http://127.0.0.1:8093'
let cookie = ''
async function call(path, body, key, expected = 200, useCookie = true) {
  const response = await fetch(`${base}/api/mvp/${path}`, { method: body === undefined ? 'GET' : 'POST', headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'StablePay', ...(useCookie && cookie ? { Cookie: cookie } : {}), ...(key ? { 'Idempotency-Key': key } : {}) }, ...(body === undefined ? {} : { body: JSON.stringify(body) }) })
  if (useCookie && response.headers.get('set-cookie')) cookie = response.headers.get('set-cookie').split(';')[0]
  const data = await response.json()
  assert.equal(response.status, expected, JSON.stringify(data))
  return data
}
const status = await call('status')
assert.equal(status.payment_mode, 'local', 'Smoke testing is limited to local simulation')
const input = { activity: '独立 HTTP 验收活动', school: '', budget: '500', budget_evidence: '合成预算依据', approved: true, items: [
  { name: '横幅', requested_spec: '3m', quoted_spec: '3m', quantity: 1, unit_price: '180', quote_source: '合成报价A', invoice_confirmed: true },
  { name: '展架', requested_spec: '80cm', quoted_spec: '60cm', quantity: 2, unit_price: '68', quote_source: '合成报价B', invoice_confirmed: true },
  { name: '水', requested_spec: '24瓶', quoted_spec: '24瓶', quantity: 4, unit_price: '35', quote_source: '合成报价C', invoice_confirmed: false },
  { name: '桌牌', requested_spec: 'A5', quoted_spec: 'A5', quantity: 4, unit_price: '20', quote_source: '合成报价D', invoice_confirmed: true },
] }
const plan = await call('plans', { prompt: '检查规格和票据', service_budget: '1.00', input })
const quote = await call('quotes', { plan_id: plan.id, product_id: 'procurement-review-v1' })
const key = randomUUID()
await call('purchases', { quote_id: quote.id, quote_hash: quote.hash, accepted: false }, key, 400)
const purchase = await call('purchases', { quote_id: quote.id, quote_hash: quote.hash, accepted: true }, key)
assert.equal(purchase.status, 'AWAITING_PAYMENT')
assert.equal(purchase.delivery, undefined)
await call(`purchases/${purchase.id}/receipt`, undefined, undefined, 409)
const replay = await call('purchases', { quote_id: quote.id, quote_hash: quote.hash, accepted: true }, key)
assert.equal(replay.payment_id, purchase.payment_id)
const done = await call(`purchases/${purchase.id}/simulate`, {})
assert.equal(done.status, 'DELIVERED')
assert.equal(done.verified_by, 'local-simulation')
assert.equal(done.delivery.report.total_minor, 53600)
assert.deepEqual(done.delivery.report.findings.map((finding) => finding.code), ['OVER_BUDGET', 'APPROVAL_UNVERIFIED', 'BUDGET_EVIDENCE_UNVERIFIED', 'QUOTE_SOURCE_UNVERIFIED', 'INVOICE_UNVERIFIED', 'QUOTE_SOURCE_UNVERIFIED', 'SPEC_MISMATCH', 'INVOICE_UNVERIFIED', 'QUOTE_SOURCE_UNVERIFIED', 'INVOICE_UNCONFIRMED', 'QUOTE_SOURCE_UNVERIFIED', 'INVOICE_UNVERIFIED'])
const receipt = await call(`purchases/${purchase.id}/receipt`)
assert.equal(receipt.delivery.content_hash, done.delivery.content_hash)
const repeat = await call(`purchases/${purchase.id}/simulate`, {})
assert.equal(repeat.events.length, done.events.length)
await call(`purchases/${purchase.id}`, undefined, undefined, 404, false)
const cheapPlan = await call('plans', { prompt: '检查', service_budget: '0.01', input })
await call('quotes', { plan_id: cheapPlan.id, product_id: 'procurement-review-v1' }, undefined, 400)
const evidence = { mode: 'local-simulation', real_alipay_transaction: false, checked_at: new Date().toISOString(), purchase_id: done.id, payment_id: done.payment_id, content_hash: done.delivery.content_hash, total_minor: 53600, findings: done.delivery.report.findings.map((finding) => finding.code), assertions: ['explicit approval', 'no premature delivery', 'idempotent order creation', 'verified local settlement', 'report integrity', 'single delivery on replay', 'session isolation', 'service budget cap'] }
if (process.argv[3]) await writeFile(process.argv[3], JSON.stringify(evidence, null, 2))
console.log(JSON.stringify(evidence, null, 2))
