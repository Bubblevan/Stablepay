// Read only the two Alipay identifiers from .env. Never evaluate or print it.
// Key material and generated configuration stay under ignored .local-run/.
import { createPrivateKey, createPublicKey, randomBytes } from 'node:crypto'
import { readFile, writeFile, mkdir } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import path from 'node:path'

const root = fileURLToPath(new URL('../', import.meta.url))
if (!process.argv[2]) throw new Error('Usage: node scripts/prepare-alipay-sandbox.mjs https://your-public-host')
const origin = new URL(process.argv[2])
if (origin.protocol !== 'https:' || origin.username || origin.password || origin.pathname !== '/' || origin.search || origin.hash) {
  throw new Error('Supply the HTTPS origin only; do not include /health or another path.')
}
const envText = (await readFile(path.join(root, '.env'), 'utf8')).replace(/^\uFEFF/, '')
function identifier(name) {
  const lines = envText.split(/\r?\n/).filter(line => new RegExp(`^\\s*(?:export\\s+)?${name}\\s*=`).test(line))
  if (lines.length !== 1) throw new Error(`${name} must appear exactly once in .env`)
  let value = lines[0].slice(lines[0].indexOf('=') + 1).trim()
  if (/^["']/.test(value)) {
    const quote = value[0]
    const end = value.indexOf(quote, 1)
    if (end < 0 || !/^\s*(?:#.*)?$/.test(value.slice(end + 1))) throw new Error(`Invalid ${name} quoting`)
    value = value.slice(1, end)
  } else value = value.replace(/\s+#.*$/, '').trim()
  if (!/^\d{10,32}$/.test(value)) throw new Error(`${name} must be a numeric sandbox identifier`)
  return value
}
async function loadKey(filename, isPrivate) {
  const source = (await readFile(path.join(root, '.local-run/secrets/alipay', filename), 'utf8')).replace(/^\uFEFF/, '').trim()
  const parse = isPrivate ? createPrivateKey : createPublicKey
  if (source.startsWith('-----BEGIN ')) return parse(source)
  const compact = source.replace(/\s/g, '')
  if (!/^[A-Za-z0-9+/]+={0,2}$/.test(compact)) throw new Error(`${filename} is neither PEM nor base64 DER`)
  const key = Buffer.from(compact, 'base64')
  for (const type of isPrivate ? ['pkcs8', 'pkcs1'] : ['spki', 'pkcs1']) {
    try { return parse({ key, format: 'der', type }) } catch { /* try the other RSA encoding */ }
  }
  throw new Error(`Cannot decode ${filename}; copy the complete key without labels or punctuation`)
}
const privateKey = await loadKey('app-private.pem', true)
const publicKey = await loadKey('alipay-public.pem', false)
for (const key of [privateKey, publicKey]) {
  if (key.asymmetricKeyType !== 'rsa' || key.asymmetricKeyDetails.modulusLength < 2048) throw new Error('RSA keys of at least 2048 bits are required')
}
if (createPublicKey(privateKey).export({ type: 'spki', format: 'pem' }) === publicKey.export({ type: 'spki', format: 'pem' })) {
  throw new Error('The Alipay public key is the application public key; obtain the separate Alipay public key from the sandbox console')
}
const appID = identifier('ALIPAY_APP_ID')
const sellerID = identifier('ALIPAY_SELLER_ID')
const output = path.join(root, '.local-run/procurement-deploy/private')
await mkdir(output, { recursive: true, mode: 0o700 })
let token
try { token = (await readFile(path.join(output, 'internal-token'), 'utf8')).trim() } catch (error) {
  if (error.code !== 'ENOENT') throw error
  token = randomBytes(32).toString('hex')
}
if (!/^[a-f0-9]{64}$/.test(token)) throw new Error('Invalid existing internal token')
const files = {
  'internal-token': `${token}\n`,
  'app-private.pem': privateKey.export({ type: 'pkcs8', format: 'pem' }),
  'alipay-public.pem': publicKey.export({ type: 'spki', format: 'pem' }),
  'app-public.pem': createPublicKey(privateKey).export({ type: 'spki', format: 'pem' }),
  'fiat.env': `FIAT_HTTP_ADDR=127.0.0.1:8092\nFIAT_PAYMENT_MODE=alipay_sandbox\nFIAT_INTERNAL_TOKEN=${token}\nFIAT_STORE_PATH=/var/lib/stablepay-mvp/payment/orders.json\nALIPAY_APP_ID=${appID}\nALIPAY_SELLER_ID=${sellerID}\nALIPAY_PRIVATE_KEY_FILE=/etc/stablepay-mvp/keys/app-private.pem\nALIPAY_PUBLIC_KEY_FILE=/etc/stablepay-mvp/keys/alipay-public.pem\nALIPAY_NOTIFY_URL=${origin.origin}/api/payments/alipay/notify\nALIPAY_RETURN_URL=${origin.origin}/payathon\n`,
  'runtime.env': `FIAT_PAYMENT_URL=http://127.0.0.1:8092\nFIAT_INTERNAL_TOKEN=${token}\nMVP_HTTP_ADDR=127.0.0.1:8093\nMVP_PUBLIC_ORIGIN=${origin.origin}\nMVP_PROFILE=payathon\nMVP_USE_LLM=0\nMVP_STORE_PATH=/var/lib/stablepay-mvp/runtime/state.json\nMVP_STATIC_DIR=/opt/stablepay-mvp/current/web\n`,
}
for (const [name, value] of Object.entries(files)) await writeFile(path.join(output, name), value, { mode: 0o600 })
console.log(JSON.stringify({ ready: true, mode: 'alipay_sandbox', origin: origin.origin, output, application_key_bits: privateKey.asymmetricKeyDetails.modulusLength, alipay_key_bits: publicKey.asymmetricKeyDetails.modulusLength, originals_unchanged: true, note: 'app-public.pem is derived for comparing with the application public key uploaded to Alipay; no key material or identifiers are printed.' }, null, 2))
