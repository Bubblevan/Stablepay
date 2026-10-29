import test from 'node:test'
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { mkdtemp, readFile, writeFile, rm } from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import { resolveAgentPaySession, orderPaymentContext, signalArguments, submitArguments, validatePaymentLink } from './alipay-agentpay-protocol.mjs'
import { runAgentPay } from './alipay-agentpay.mjs'

// Synthetic fixtures only. All native commands and HTTP calls are replaced.
const sessionID = '12345678-1234-4234-8234-123456789abc'
const otherSessionID = '87654321-4321-4321-8321-cba987654321'
const purchaseID = '0123456789abcdef0123456789abcdef'
const digest = value => createHash('sha256').update(value).digest('hex')
const purchase = () => ({ id:purchaseID, status:'AWAITING_PAYMENT', approval_ref:'approved-fixture', quote:{ hash:'quote-fixture', mode:'alipay_sandbox', product:{ currency:'CNY', amount_minor:1, name:'活动预算核算', merchant:'StablePay 自营工具' } } })
const session = { id:sessionID, source:'CODEX_THREAD_ID' }
const paymentLink = 'opaque-fixture://Exact%2fCase+and=Encoding&x=%2525'

function preparedState(p = purchase()) {
  const context = orderPaymentContext(p)
  return { stage:'SIGNAL_READY', purchase_id:p.id, agent_session_id:sessionID, quote_hash:p.quote.hash, approval_ref:p.approval_ref, amount:context.amount, merchant_info:context.merchantInfo, payment_link:paymentLink, link_digest:digest(paymentLink) }
}

test('session priority: AIPAY_SESSION_ID, CODEX_THREAD_ID, CODEX_SESSION_ID', () => {
  assert.deepEqual(resolveAgentPaySession({AIPAY_SESSION_ID:sessionID,CODEX_THREAD_ID:otherSessionID}),{id:sessionID,source:'AIPAY_SESSION_ID'})
  assert.deepEqual(resolveAgentPaySession({CODEX_THREAD_ID:sessionID,CODEX_SESSION_ID:otherSessionID}),session)
  assert.deepEqual(resolveAgentPaySession({CODEX_SESSION_ID:sessionID}),{id:sessionID,source:'CODEX_SESSION_ID'})
})

test('missing, empty, nil, invented and purchase IDs fail without fallback', () => {
  assert.throws(() => resolveAgentPaySession({}), /No real Agent session UUID/)
  for (const value of ['',purchaseID,'session-123','20261029','00000000-0000-0000-0000-000000000000',` ${sessionID}`,`${sessionID}\n`]) {
    assert.throws(() => resolveAgentPaySession({AIPAY_SESSION_ID:value,CODEX_THREAD_ID:sessionID}), /real current Agent session UUID/)
  }
})

test('intent is freshly extracted from the confirmed quote in the official format', () => {
  const context = orderPaymentContext(purchase())
  assert.equal(context.intentSummary,'服务内容：活动预算核算，支付金额：¥0.01，支付对象：StablePay 自营工具')
  const p = purchase(); p.quote.product.amount_minor = 53600
  assert.equal(orderPaymentContext(p).amount,'536.00')
  for (const amount of [0,-1,1.1,Number.MAX_SAFE_INTEGER+1,'1']) {
    p.quote.product.amount_minor = amount
    assert.throws(() => orderPaymentContext(p), /integer fen/)
  }
})

test('unknown currency, missing merchant and multiline service are rejected', () => {
  const p = purchase(); p.quote.product.currency = 'USDC'
  assert.throws(() => orderPaymentContext(p), /CNY/)
  for (const [field,value] of [['merchant',''],['name','服务\n另一个摘要']]) {
    const q = purchase(); q.quote.product[field] = value
    assert.throws(() => orderPaymentContext(q), /unambiguous/)
  }
})

test('signal and submit use the same real session, never the business purchase ID', () => {
  const p = purchase()
  const signal = signalArguments({cashierURL:'https://fixture.invalid',context:orderPaymentContext(p),session})
  const submit = submitArguments({state:preparedState(p),purchase:p,session,digest})
  assert.equal(signal[signal.indexOf('--session-id')+1],sessionID)
  assert.equal(submit[submit.indexOf('--session-id')+1],sessionID)
  assert.ok(!signal.includes(purchaseID)); assert.ok(!submit.includes(purchaseID))
  assert.equal(submit[submit.indexOf('--payment-link')+1],paymentLink)
})

test('paymentLink is preserved byte for byte and malformed links fail', () => {
  assert.equal(validatePaymentLink(paymentLink),paymentLink)
  assert.equal(validatePaymentLink(' opaque '),' opaque ')
  for (const value of ['',null,'\nlink','link\0','x'.repeat(32769)]) assert.throws(() => validatePaymentLink(value))
})

test('session switch and legacy handoff are blocked', () => {
  const state = preparedState()
  assert.throws(() => submitArguments({state,purchase:purchase(),session:{id:otherSessionID},digest}), /session changed/)
  delete state.agent_session_id
  assert.throws(() => submitArguments({state,purchase:purchase(),session,digest}), /Legacy handoff/)
})

test('quote, approval, amount, merchant, link and prior submit remain immutable', () => {
  for (const [field,value] of [['purchase_id','other'],['quote_hash','other'],['approval_ref','other'],['amount','0.02'],['merchant_info','other'],['payment_link','changed'],['stage','SUBMIT_REQUESTED']]) {
    const state = {...preparedState(),[field]:value}
    assert.throws(() => submitArguments({state,purchase:purchase(),session,digest}), /differs/)
  }
  const p = purchase(); p.quote.product.name = '另一项服务'
  assert.throws(() => submitArguments({state:preparedState(),purchase:p,session,digest}), /differs/)
})

async function fixture(t) {
  const base = path.resolve(os.tmpdir())
  const dir = await mkdtemp(path.join(base,'stablepay-agentpay-test-'))
  t.after(async () => {
    if (path.dirname(dir) !== base || !path.basename(dir).startsWith('stablepay-agentpay-test-')) throw new Error('Unexpected test cleanup path')
    await rm(dir,{recursive:true,force:true})
  })
  const calls = [], commands = [], p = purchase()
  const call = async (endpoint,body) => {
    calls.push({endpoint,body})
    if (endpoint === `purchases/${purchaseID}`) return p
    if (endpoint.endsWith('/agentpay-handoff')) return {purchase_id:purchaseID,quote_hash:p.quote.hash,approval_ref:p.approval_ref,payment_mode:'alipay_sandbox',harness_policy:{decision:'ALLOW'},cashier_url:'https://openapi-sandbox.dl.alipaydev.com/gateway.do?method=alipay.trade.page.pay',amount:'0.01',merchant_info:orderPaymentContext(p).merchantInfo}
    if (endpoint.endsWith('/agentpay-claim')) return {id:`fixture-${body.stage}`}
    if (endpoint.endsWith('/agentpay-result')) return {}
    throw new Error('Unexpected fixture endpoint')
  }
  const execute = async (_file,args) => {
    commands.push(args)
    if (args[0] === '--version') return {stdout:'fixture-cli'}
    if (args[0] === 'trigger-payment-signal') return {stdout:JSON.stringify({success:true,paymentLink})}
    if (args[0] === 'submit-payment') return {stdout:JSON.stringify({success:true})}
    throw new Error('Unexpected native command (no real command can execute)')
  }
  const options = {purchaseId:purchaseID,call,sessionDir:dir,environment:{CODEX_THREAD_ID:sessionID},execute}
  return {dir,calls,commands,options}
}

test('mocked prepare/submit binds context and never calls check-wallet', async t => {
  const f = await fixture(t)
  await runAgentPay({...f.options,command:'agentpay-prepare'})
  const state = JSON.parse(await readFile(path.join(f.dir,'agentpay',`${purchaseID}.json`),'utf8'))
  assert.equal(state.agent_session_id,sessionID)
  assert.equal(state.agent_session_source,'CODEX_THREAD_ID')
  assert.equal(state.payment_link,paymentLink)
  await runAgentPay({...f.options,command:'agentpay-submit',confirm:true})
  assert.deepEqual(f.commands.map(args => args[0]),['--version','trigger-payment-signal','--version','submit-payment'])
  const args = f.commands.at(-1)
  assert.equal(args[args.indexOf('--intent-summary')+1],orderPaymentContext(purchase()).intentSummary)
  assert.equal(args[args.indexOf('--session-id')+1],sessionID)
  assert.equal(args[args.indexOf('--payment-link')+1],paymentLink)
  await assert.rejects(runAgentPay({...f.options,command:'agentpay-submit',confirm:true}), /differs/)
  assert.equal(f.commands.filter(args => args[0] === 'submit-payment').length,1)
  assert.equal(f.calls.filter(c => c.endpoint.endsWith('/agentpay-claim') && c.body.stage === 'submit').length,1)
})

test('missing session stops before any server call, native command or claim', async t => {
  const f = await fixture(t)
  await assert.rejects(runAgentPay({...f.options,command:'agentpay-prepare',environment:{}}), /No real Agent session UUID/)
  assert.equal(f.calls.length,0); assert.equal(f.commands.length,0)
})

test('no confirmation, legacy handoff or changed session cannot take a submit claim', async t => {
  const f = await fixture(t)
  await assert.rejects(runAgentPay({...f.options,command:'agentpay-submit'}), /requires --confirm/)
  assert.equal(f.calls.length,0)
  await runAgentPay({...f.options,command:'agentpay-prepare'})
  await assert.rejects(runAgentPay({...f.options,command:'agentpay-submit',confirm:true,environment:{CODEX_THREAD_ID:otherSessionID}}), /session changed/)
  const file = path.join(f.dir,'agentpay',`${purchaseID}.json`)
  const state = JSON.parse(await readFile(file,'utf8')); delete state.agent_session_id
  await writeFile(file,JSON.stringify(state))
  await assert.rejects(runAgentPay({...f.options,command:'agentpay-submit',confirm:true}), /Legacy handoff/)
  assert.equal(f.calls.filter(c => c.endpoint.endsWith('/agentpay-claim') && c.body.stage === 'submit').length,0)
})

test('official rejection is retained as UNKNOWN with no automatic retry', async t => {
  const f = await fixture(t)
  await runAgentPay({...f.options,command:'agentpay-prepare'})
  const execute = async (_file,args) => {
    if (args[0] === '--version') return {stdout:'fixture-cli'}
    const error = new Error('fixture rejected'); error.stdout = JSON.stringify({success:false,errorCode:'SIGN_VERIFY_FAILED',errorMsg:'签名校验失败'}); throw error
  }
  await assert.rejects(runAgentPay({...f.options,command:'agentpay-submit',confirm:true,execute}), /SIGN_VERIFY_FAILED/)
  assert.equal(f.calls.at(-1).body.status,'UNKNOWN')
  const state = JSON.parse(await readFile(path.join(f.dir,'agentpay',`${purchaseID}.json`),'utf8'))
  assert.equal(state.stage,'SUBMIT_REQUESTED')
  assert.equal(JSON.parse(await readFile(path.join(f.dir,'agentpay','fixture-submit.json'),'utf8')).errorCode,'SIGN_VERIFY_FAILED')
})
