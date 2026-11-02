import { createHash, createPrivateKey, createSign, randomUUID } from 'node:crypto';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { AlipaySdk } from 'alipay-sdk';

export const A2M_PATH = '/a2m/execute';
export const A2M_SANDBOX_GATEWAY = 'https://openapi-sandbox.dl.alipaydev.com/gateway.do';
export const A2M_SANDBOX_SERVICE_ID = 'api_mock_service_id';

const PROJECT_ROOT = path.dirname(fileURLToPath(import.meta.url));
const SANDBOX_CONFIG_PATH = path.join(PROJECT_ROOT, '.alipay-sandbox.json');

function integrationError(code, message) {
  const error = new Error(message);
  error.code = code;
  return error;
}

function readSandboxConfiguration() {
  let sandbox;
  try {
    sandbox = JSON.parse(readFileSync(SANDBOX_CONFIG_PATH, 'utf8'));
  } catch (error) {
    if (error?.code === 'ENOENT') {
      throw integrationError('A2M_SANDBOX_CONFIG_NOT_READY', 'Alipay sandbox configuration is not ready');
    }
    throw integrationError('A2M_SANDBOX_CONFIG_INVALID', 'Alipay sandbox configuration could not be read');
  }

  const app = sandbox?.appIds?.[0];
  const partner = sandbox?.sandboxAccounts?.partner;
  const appId = app?.appId;
  const privateKey = app?.appPrivatePkcsKey;
  const alipayPublicKey = app?.alipayPublicKey;
  const sellerId = app?.pid;
  const requiredValues = [appId, privateKey, alipayPublicKey, sellerId];
  if (!requiredValues.every((value) => typeof value === 'string' && value.length > 0)) {
    throw integrationError('A2M_SANDBOX_CONFIG_INVALID', 'Alipay sandbox app or merchant fields are incomplete');
  }
  if (!/^2088\d+$/u.test(sellerId)) {
    throw integrationError('A2M_SANDBOX_CONFIG_INVALID', 'Alipay sandbox merchant ID is invalid');
  }
  if ([privateKey, alipayPublicKey].some((value) => value !== value.trim() || /-----BEGIN|-----END|[\r\n]/u.test(value))) {
    throw integrationError('A2M_SANDBOX_CONFIG_INVALID', 'Alipay key fields must remain in their original raw format');
  }

  return {
    appId,
    privateKey,
    alipayPublicKey,
    sellerId,
    sellerName: typeof partner.accountDesc === 'string' && partner.accountDesc
      ? partner.accountDesc
      : 'Sandbox merchant',
  };
}

function makeAlipaySdk(configuration) {
  return new AlipaySdk({
    appId: configuration.appId,
    privateKey: configuration.privateKey,
    alipayPublicKey: configuration.alipayPublicKey,
    gateway: A2M_SANDBOX_GATEWAY,
    signType: 'RSA2',
    timeout: 30000,
  });
}

function normalizeAmount(value) {
  const match = /^(\d+)(?:\.(\d{1,2}))?$/u.exec(String(value ?? '').trim());
  if (!match) return null;
  return `${BigInt(match[1]).toString()}.${(match[2] || '').padEnd(2, '0')}`;
}

function formatISO8601WithTimezone(date) {
  const pad = (value) => String(value).padStart(2, '0');
  const offset = -date.getTimezoneOffset();
  const offsetHours = pad(Math.floor(Math.abs(offset) / 60));
  const offsetMinutes = pad(Math.abs(offset) % 60);
  const sign = offset >= 0 ? '+' : '-';
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
    + `T${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
    + `${sign}${offsetHours}:${offsetMinutes}`;
}

function base64UrlEncode(value) {
  return Buffer.from(value, 'utf8').toString('base64url');
}

function decodePaymentProof(value) {
  if (typeof value !== 'string' || value.length === 0 || value.length > 32768 || !/^[A-Za-z0-9_-]+$/u.test(value)) {
    return null;
  }
  try {
    const parsed = JSON.parse(Buffer.from(value, 'base64url').toString('utf8'));
    const paymentProof = parsed?.protocol?.payment_proof;
    const tradeNo = parsed?.protocol?.trade_no;
    const clientSession = parsed?.method?.client_session;
    if (typeof paymentProof !== 'string' || !paymentProof || typeof tradeNo !== 'string' || !tradeNo) return null;
    return {
      paymentProof,
      tradeNo,
      ...(typeof clientSession === 'string' && clientSession ? { clientSession } : {}),
    };
  } catch {
    return null;
  }
}

function signSellerBill(fields, rawPrivateKey) {
  const content = Object.keys(fields)
    .sort()
    .filter((key) => fields[key] !== null && fields[key] !== undefined && fields[key] !== '')
    .map((key) => `${key}=${fields[key]}`)
    .join('&');
  const privateKey = createPrivateKey({
    key: Buffer.from(rawPrivateKey, 'base64'),
    format: 'der',
    type: 'pkcs1',
  });
  return createSign('RSA-SHA256').update(content, 'utf8').sign(privateKey, 'base64');
}

function currentResourceId(agentDid, requestText) {
  const fingerprint = createHash('sha256')
    .update(`${agentDid || ''}\n${requestText}`, 'utf8')
    .digest('hex')
    .slice(0, 24);
  return `${A2M_PATH}?request=${fingerprint}`;
}

function sendJson(res, status, payload, headers = {}) {
  res.statusCode = status;
  res.setHeader('Content-Type', 'application/json; charset=utf-8');
  for (const [name, value] of Object.entries(headers)) res.setHeader(name, value);
  res.end(JSON.stringify(payload));
}

function apiSucceeded(response) {
  return response?.code === '10000' || response?.code === 10000;
}

function unwrapResponse(response, key) {
  return response?.[key] || response;
}

export function createA2MPaymentHandler({ repository, createResource, validateResource, env = process.env }) {
  let runtimePromise;

  async function getRuntime() {
    if (!runtimePromise) {
      runtimePromise = Promise.resolve().then(() => {
        const configuration = readSandboxConfiguration();
        const amount = normalizeAmount(env.A2M_AMOUNT_CNY || '0.01');
        if (!amount || Number(amount) <= 0) {
          throw integrationError('A2M_CONFIGURATION_INVALID', 'A2M_AMOUNT_CNY must be a positive CNY amount');
        }
        return {
          configuration,
          amount,
          sdk: makeAlipaySdk(configuration),
        };
      }).catch((error) => {
        runtimePromise = null;
        throw error;
      });
    }
    return runtimePromise;
  }

  async function createPaymentRequiredResponse(res, runtime, { resourceId, requestText }) {
    const { configuration, amount } = runtime;
    const outTradeNo = `A2M_${Date.now()}_${randomUUID().replaceAll('-', '').slice(0, 16)}`;
    const currency = 'CNY';
    const goodsName = env.A2M_GOODS_NAME || `${env.SKILL_NAME || 'ShowMeTheMoney Pro'} API access`;
    const payBefore = formatISO8601WithTimezone(new Date(Date.now() + 30 * 60 * 1000));
    const signatureFields = {
      amount,
      currency,
      goods_name: goodsName,
      out_trade_no: outTradeNo,
      pay_before: payBefore,
      resource_id: resourceId,
      seller_id: configuration.sellerId,
      service_id: A2M_SANDBOX_SERVICE_ID,
    };
    const sellerSignature = signSellerBill(signatureFields, configuration.privateKey);

    await repository.createPending({
      outTradeNo,
      amount,
      currency,
      resourceId,
      goodsName,
      payBefore,
    });

    const paymentNeeded = {
      protocol: {
        out_trade_no: outTradeNo,
        amount,
        currency,
        resource_id: resourceId,
        pay_before: payBefore,
        seller_signature: sellerSignature,
        seller_sign_type: 'RSA2',
        seller_unique_id: configuration.sellerId,
      },
      method: {
        seller_name: configuration.sellerName,
        seller_id: configuration.sellerId,
        seller_app_id: configuration.appId,
        goods_name: goodsName,
        seller_unique_id_key: 'seller_id',
        service_id: A2M_SANDBOX_SERVICE_ID,
      },
    };
    sendJson(res, 402, {
      code: 'Payment-Needed',
      message: 'Payment required to use this API',
      amount,
      currency,
      resource_id: resourceId,
      request_text: requestText,
    }, { 'Payment-Needed': base64UrlEncode(JSON.stringify(paymentNeeded)) });
  }

  return async function handleA2MRequest(req, res, url) {
    const agentDid = url.searchParams.get('agent_did') || undefined;
    if (!repository || typeof repository.createPending !== 'function'
      || typeof repository.findByOutTradeNo !== 'function'
      || typeof repository.prepareFulfillment !== 'function'
      || typeof repository.markFulfilled !== 'function') {
      sendJson(res, 503, { code: 'A2M_ORDER_STORE_UNAVAILABLE', message: 'A2M order storage is not configured' });
      return;
    }

    const requestText = url.searchParams.get('q') || url.searchParams.get('prompt')
      || url.searchParams.get('gift') || 'default-premium-request';
    const resourceId = currentResourceId(agentDid, requestText);

    try {
      if (typeof validateResource === 'function') await validateResource();
      const runtime = await getRuntime();
      const paymentProofHeader = req.headers['payment-proof'];
      if (typeof paymentProofHeader !== 'string' || paymentProofHeader.trim() === '') {
        await createPaymentRequiredResponse(res, runtime, { resourceId, requestText });
        return;
      }

      const proof = decodePaymentProof(paymentProofHeader);
      if (!proof) {
        await createPaymentRequiredResponse(res, runtime, { resourceId, requestText });
        return;
      }

      const verifyResult = await runtime.sdk.exec('alipay.aipay.agent.payment.verify', {
        bizContent: {
          trade_no: proof.tradeNo,
          payment_proof: proof.paymentProof,
          ...(proof.clientSession ? { client_session: proof.clientSession } : {}),
        },
      });
      const verification = unwrapResponse(verifyResult, 'alipay_aipay_agent_payment_verify_response');
      if (!apiSucceeded(verification)) {
        await createPaymentRequiredResponse(res, runtime, { resourceId, requestText });
        return;
      }

      const verifiedTradeNo = verification.trade_no || verification.tradeNo;
      const outTradeNo = verification.out_trade_no || verification.outTradeNo;
      const verifiedResourceId = verification.resource_id || verification.resourceId;
      const order = outTradeNo ? await repository.findByOutTradeNo(outTradeNo) : null;
      const amountMatches = order
        && normalizeAmount(order.amount) !== null
        && normalizeAmount(verification.amount) === normalizeAmount(order.amount);
      const resourceMatches = order && order.resourceId === resourceId && verifiedResourceId === resourceId;
      const stateUsable = order
        && order.currency === 'CNY'
        && ['PENDING_PAYMENT', 'PAID'].includes(order.orderStatus)
        && ['UNFULFILLED', 'PENDING_CONFIRM', 'FULFILLED'].includes(order.fulfillStatus);

      if (verification.active !== true || !verifiedTradeNo || verifiedTradeNo !== proof.tradeNo
        || !outTradeNo || !amountMatches || !resourceMatches || !stateUsable) {
        await createPaymentRequiredResponse(res, runtime, { resourceId, requestText });
        return;
      }

      const fulfillment = await repository.prepareFulfillment({
        outTradeNo,
        tradeNo: verifiedTradeNo,
        expectedAmount: normalizeAmount(order.amount),
        expectedResourceId: resourceId,
        createResource: () => createResource({ agentDid, requestText, resourceId }),
      });
      if (!fulfillment?.serviceResult || !['PENDING_CONFIRM', 'FULFILLED'].includes(fulfillment.state)) {
        throw new Error('A2M order storage did not persist a fulfillment result');
      }

      if (fulfillment.state !== 'FULFILLED') {
        const confirmResult = await runtime.sdk.exec('alipay.aipay.agent.fulfillment.confirm', {
          bizContent: { trade_no: verifiedTradeNo },
        });
        const confirmation = unwrapResponse(confirmResult, 'alipay_aipay_agent_fulfillment_confirm_response');
        if (!apiSucceeded(confirmation)) {
          sendJson(res, 502, {
            code: 'FULFILLMENT_CONFIRM_FAILED',
            message: 'The resource is saved; retry the same Payment-Proof to confirm fulfillment',
          });
          return;
        }
        await repository.markFulfilled(outTradeNo, verifiedTradeNo);
      }

      sendJson(res, 200, fulfillment.serviceResult, {
        'Payment-Validation': base64UrlEncode(JSON.stringify({
          trade_no: verifiedTradeNo,
          out_trade_no: outTradeNo,
          validated: true,
          resource_id: resourceId,
        })),
      });
    } catch (error) {
      if (error?.code === 'A2M_SANDBOX_CONFIG_NOT_READY') {
        sendJson(res, 503, { code: 'ALIPAY_SANDBOX_CONFIG_REQUIRED', message: 'Alipay sandbox configuration is not ready' });
        return;
      }
      if (error?.code === 'A2M_SANDBOX_CONFIG_INVALID') {
        sendJson(res, 503, { code: 'ALIPAY_SANDBOX_CONFIG_INVALID', message: 'Alipay sandbox configuration is invalid' });
        return;
      }
      if (error?.code === 'A2M_DATABASE_NOT_CONFIGURED') {
        sendJson(res, 503, { code: 'A2M_ORDER_STORE_NOT_CONFIGURED', message: 'A2M MySQL order storage is not configured' });
        return;
      }
      if (error?.code === 'A2M_RESOURCE_NOT_CONFIGURED') {
        sendJson(res, 503, { code: 'A2M_RESOURCE_NOT_CONFIGURED', message: 'Merchant proof generation is not configured' });
        return;
      }
      console.error('[a2m] request failed:', error?.code || 'unexpected_error');
      sendJson(res, 503, { code: 'A2M_TEMPORARILY_UNAVAILABLE', message: 'A2M payment service is temporarily unavailable' });
    }
  };
}
