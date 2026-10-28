// Explicit real-model end-to-end acceptance. Use only synthetic source texts.
import assert from 'node:assert/strict'
import { writeFile, mkdir } from 'node:fs/promises'
import { requirementsExample, quotationsExample, sourceExcerpt } from '../stablepay-frontend/src/campusModel.mjs'
const base = process.argv[2] || 'http://127.0.0.1:8093'
const output = process.argv[3] || '.local-run/campus-validation'
let cookie = ''
async function call(path, body, expected = 200, session = true, origin = base) {
  const response = await fetch(base + '/api/mvp/' + path, { method: body === undefined ? 'GET' : 'POST', headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'StablePay', Origin: origin, ...(session && cookie ? { Cookie: cookie } : {}) }, ...(body === undefined ? {} : { body: JSON.stringify(body) }), signal: AbortSignal.timeout(45000) })
  if (session && response.headers.get('set-cookie')) cookie = response.headers.get('set-cookie').split(';')[0]
  const data = await response.json()
  assert.equal(response.status, expected, JSON.stringify(data))
  return data
}
await mkdir(output, { recursive: true })
const status = await call('campus/status')
assert.equal(status.extraction_configured, true, 'Real-model configuration is required; no mock fallback is accepted')
const started = Date.now()
const extraction = await call('extractions', { requirements: requirementsExample, quotations: quotationsExample, demo: true })
assert.equal(extraction.provider === 'test-fixture', false)
assert.equal(extraction.draft.approved, false)
assert.equal(extraction.draft.items.every(item => item.invoice_confirmed === false), true)
assert.equal(extraction.draft.items.length, 4)
for (const field of extraction.fields) {
  const source = field.source === 'requirements' ? requirementsExample : quotationsExample
  assert.notEqual(sourceExcerpt(source, field), null, 'Every model value must have an exact source reference')
}
assert.equal(Number(extraction.draft.budget), 500)
const input = extraction.draft
await call('campus/reviews', { input, extraction_id: extraction.id, confirmed: false }, 400)
const first = await call('campus/reviews', { input, extraction_id: extraction.id, confirmed: true })
assert.equal(first.report.total_minor, 53600)
assert.equal(first.report.findings.some(f => f.code === 'OVER_BUDGET'), true)
const banner = input.items.findIndex(item => item.name.includes('横幅'))
const stand = input.items.findIndex(item => item.name.includes('展架'))
assert.ok(banner >= 0 && stand >= 0)
assert.equal(first.report.findings.some(f => f.code === 'SPEC_MISMATCH' && f.item_index === banner), false)
assert.equal(first.report.findings.some(f => f.code === 'SPEC_MISMATCH' && f.item_index === stand), true)
await call('extractions/' + extraction.id, undefined, 404, false)
await call('campus/reviews/' + first.id, undefined, 404, false)
await call('campus/reviews', { input, confirmed: true }, 403, true, 'https://evil.example')
const corrected = structuredClone(input)
corrected.items[banner].unit_price = '140'
corrected.items[stand].quoted_spec = corrected.items[stand].requested_spec
const second = await call('campus/reviews', { input: corrected, extraction_id: extraction.id, confirmed: true })
assert.equal(second.report.total_minor, 49600)
assert.equal(second.report.findings.some(f => ['OVER_BUDGET','SPEC_MISMATCH'].includes(f.code)), false)
assert.equal(second.report.findings.some(f => f.code === 'APPROVAL_PENDING'), true)
assert.equal(second.report.findings.some(f => f.code === 'BUDGET_EVIDENCE_UNVERIFIED'), true)
assert.ok(second.changes_after_extraction.length >= 2)
assert.deepEqual((await call('campus/reviews/' + second.id)).report, second.report)
const downloaded = await fetch(base + '/api/mvp/campus/reviews/' + second.id + '/export', { headers: { Cookie: cookie } })
assert.equal(downloaded.status, 200)
const markdown = await downloaded.text()
assert.ok(markdown.includes(extraction.response_hash) && markdown.includes('当时粘贴的原文') && markdown.includes('items['))
await writeFile(output + '/report.md', markdown)
await writeFile(output + '/extraction.json', JSON.stringify(extraction, null, 2))
await writeFile(output + '/session.json', JSON.stringify({ cookie, review_id: second.id, extraction_id: extraction.id }), { mode: 0o600 })
const evidence = { checked_at: new Date().toISOString(), scope: 'real model with synthetic procurement texts; localhost only', public_deployment_verified: false, actual_student_feedback: false, real_payment: false, provider: extraction.provider, model: extraction.model, usage: extraction.usage, extraction_id: extraction.id, response_hash: extraction.response_hash, field_references: extraction.fields.length, elapsed_ms: Date.now()-started, first_total_minor: first.report.total_minor, corrected_total_minor: second.report.total_minor, report_id: second.id, remaining_findings: second.report.findings.map(f => f.code), assertions: ['real model extraction', 'exact original text references', 'no AI approval or invoice verification', 'explicit confirmation', 'equivalent unit conversion', 'budget and specification correction', 'missing evidence remains flagged', 'cross-session isolation', 'cross-origin rejection', 'persisted report retrieval', 'export includes original evidence and edits'] }
await writeFile(output + '/validation.json', JSON.stringify(evidence, null, 2))
console.log(JSON.stringify(evidence, null, 2))
