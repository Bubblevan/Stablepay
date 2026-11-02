import mysql from 'mysql2/promise';
import { parse as parseDotenv } from 'dotenv';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const PROJECT_ROOT = path.dirname(fileURLToPath(import.meta.url));

function localStablePayDatabaseEnvironment(env) {
  const completeA2mMySqlConfiguration = env.A2M_MYSQL_HOST && env.A2M_MYSQL_USER
    && env.A2M_MYSQL_PASSWORD && env.A2M_MYSQL_DATABASE;
  if (env.A2M_MYSQL_URL || env.A2M_MYSQL_DSN || env.COMMERCE_RUNTIME_MYSQL_DSN
    || completeA2mMySqlConfiguration || env.NODE_ENV === 'production') {
    return env;
  }

  // Reuse only the existing MySQL DSN from StablePay's parent .env in local development.
  // Other values in that file stay out of this process's application configuration.
  try {
    const rootEnvPath = path.resolve(PROJECT_ROOT, '../..', '.env');
    const rootEnv = parseDotenv(readFileSync(rootEnvPath, 'utf8'));
    if (rootEnv.COMMERCE_RUNTIME_MYSQL_DSN) {
      return { ...env, A2M_MYSQL_DSN: rootEnv.COMMERCE_RUNTIME_MYSQL_DSN };
    }
  } catch {
    // Explicit A2M_MYSQL_* variables remain the supported fallback.
  }
  return env;
}

function configurationError(message) {
  const error = new Error(message);
  error.code = 'A2M_DATABASE_NOT_CONFIGURED';
  return error;
}

function parseGoMysqlDsn(dsn) {
  const marker = '@tcp(';
  const markerIndex = dsn.indexOf(marker);
  if (markerIndex <= 0) throw configurationError('A2M MySQL DSN format is invalid');

  const credentials = dsn.slice(0, markerIndex);
  const separator = credentials.indexOf(':');
  if (separator <= 0) throw configurationError('A2M MySQL DSN credentials are invalid');

  const addressStart = markerIndex + marker.length;
  const addressEnd = dsn.indexOf(')', addressStart);
  if (addressEnd < 0) throw configurationError('A2M MySQL DSN address is invalid');

  const address = dsn.slice(addressStart, addressEnd);
  const portSeparator = address.lastIndexOf(':');
  const databaseStart = addressEnd + 2;
  const queryStart = dsn.indexOf('?', databaseStart);
  const database = dsn.slice(databaseStart, queryStart < 0 ? undefined : queryStart);
  const port = portSeparator < 0 ? 3306 : Number(address.slice(portSeparator + 1));
  const host = portSeparator < 0 ? address : address.slice(0, portSeparator);

  if (!host || !Number.isInteger(port) || port < 1 || port > 65535 || !database) {
    throw configurationError('A2M MySQL DSN address or database is invalid');
  }

  return {
    host,
    port,
    user: credentials.slice(0, separator),
    password: credentials.slice(separator + 1),
    database,
  };
}

function mysqlConfiguration(env) {
  const uri = env.A2M_MYSQL_URL;
  if (uri) {
    let parsed;
    try {
      parsed = new URL(uri);
    } catch {
      throw configurationError('A2M_MYSQL_URL is invalid');
    }
    if (parsed.protocol !== 'mysql:' || !parsed.hostname || !parsed.pathname.slice(1)) {
      throw configurationError('A2M_MYSQL_URL must include a MySQL host and database');
    }
    return {
      host: parsed.hostname,
      port: Number(parsed.port || 3306),
      user: decodeURIComponent(parsed.username),
      password: decodeURIComponent(parsed.password),
      database: decodeURIComponent(parsed.pathname.slice(1)),
    };
  }

  const dsn = env.A2M_MYSQL_DSN || env.COMMERCE_RUNTIME_MYSQL_DSN;
  if (dsn) return parseGoMysqlDsn(dsn);

  const { A2M_MYSQL_HOST, A2M_MYSQL_PORT, A2M_MYSQL_USER, A2M_MYSQL_PASSWORD, A2M_MYSQL_DATABASE } = env;
  if (!A2M_MYSQL_HOST || !A2M_MYSQL_USER || !A2M_MYSQL_PASSWORD || !A2M_MYSQL_DATABASE) {
    throw configurationError('Set A2M_MYSQL_URL, A2M_MYSQL_DSN, or all A2M_MYSQL_* connection variables');
  }

  const port = Number(A2M_MYSQL_PORT || 3306);
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    throw configurationError('A2M_MYSQL_PORT is invalid');
  }

  return {
    host: A2M_MYSQL_HOST,
    port,
    user: A2M_MYSQL_USER,
    password: A2M_MYSQL_PASSWORD,
    database: A2M_MYSQL_DATABASE,
  };
}

function normalizeOrder(row) {
  if (!row) return null;
  let serviceResult = row.service_result;
  if (typeof serviceResult === 'string') {
    try {
      serviceResult = JSON.parse(serviceResult);
    } catch {
      serviceResult = null;
    }
  }
  return {
    outTradeNo: row.out_trade_no,
    tradeNo: row.trade_no,
    amount: String(row.amount),
    currency: row.currency,
    resourceId: row.resource_id,
    goodsName: row.goods_name,
    payBefore: row.pay_before,
    orderStatus: row.order_status,
    fulfillStatus: row.fulfill_status,
    serviceResult,
  };
}

export function createA2MOrderRepository({ env = process.env } = {}) {
  const databaseEnvironment = localStablePayDatabaseEnvironment(env);
  let pool;
  let schemaPromise;

  function getPool() {
    if (!pool) {
      pool = mysql.createPool({
        ...mysqlConfiguration(databaseEnvironment),
        waitForConnections: true,
        connectionLimit: 5,
        queueLimit: 0,
        charset: 'utf8mb4',
      });
    }
    return pool;
  }

  async function ensureSchema() {
    if (!schemaPromise) {
      schemaPromise = getPool().execute(`
        CREATE TABLE IF NOT EXISTS a2m_orders (
          out_trade_no VARCHAR(64) NOT NULL,
          trade_no VARCHAR(64) NULL,
          amount DECIMAL(18, 2) NOT NULL,
          currency CHAR(3) NOT NULL,
          resource_id VARCHAR(512) NOT NULL,
          goods_name VARCHAR(128) NOT NULL,
          pay_before VARCHAR(35) NOT NULL,
          order_status VARCHAR(24) NOT NULL,
          fulfill_status VARCHAR(24) NOT NULL,
          service_result JSON NULL,
          created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
          updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
          PRIMARY KEY (out_trade_no),
          UNIQUE KEY uq_a2m_trade_no (trade_no),
          KEY ix_a2m_order_state (order_status, fulfill_status)
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin
      `).catch((error) => {
        schemaPromise = null;
        throw error;
      });
    }
    await schemaPromise;
  }

  return {
    async createPending(order) {
      await ensureSchema();
      await getPool().execute(
        `INSERT INTO a2m_orders
          (out_trade_no, amount, currency, resource_id, goods_name, pay_before, order_status, fulfill_status)
         VALUES (?, ?, ?, ?, ?, ?, 'PENDING_PAYMENT', 'UNFULFILLED')`,
        [order.outTradeNo, order.amount, order.currency, order.resourceId, order.goodsName, order.payBefore],
      );
    },

    async findByOutTradeNo(outTradeNo) {
      await ensureSchema();
      const [rows] = await getPool().execute(
        `SELECT out_trade_no, trade_no, amount, currency, resource_id, goods_name, pay_before,
                order_status, fulfill_status, service_result
           FROM a2m_orders WHERE out_trade_no = ? LIMIT 1`,
        [outTradeNo],
      );
      return normalizeOrder(rows[0]);
    },

    async prepareFulfillment({ outTradeNo, tradeNo, expectedAmount, expectedResourceId, createResource }) {
      await ensureSchema();
      const connection = await getPool().getConnection();
      try {
        await connection.beginTransaction();
        const [rows] = await connection.execute(
          `SELECT out_trade_no, trade_no, amount, currency, resource_id, goods_name, pay_before,
                  order_status, fulfill_status, service_result
             FROM a2m_orders WHERE out_trade_no = ? FOR UPDATE`,
          [outTradeNo],
        );
        const order = normalizeOrder(rows[0]);
        if (!order) throw new Error('A2M order does not exist');
        if (order.currency !== 'CNY' || order.amount !== expectedAmount || order.resourceId !== expectedResourceId) {
          throw new Error('A2M order does not match the verified payment');
        }
        if (order.tradeNo && order.tradeNo !== tradeNo) throw new Error('A2M order is bound to another trade');

        if (order.fulfillStatus === 'FULFILLED') {
          if (!order.serviceResult) throw new Error('Fulfilled A2M order has no saved result');
          await connection.commit();
          return { state: 'FULFILLED', serviceResult: order.serviceResult };
        }

        const retryingConfirmation = order.fulfillStatus === 'PENDING_CONFIRM';
        if (!retryingConfirmation) {
          if (order.fulfillStatus !== 'UNFULFILLED' || order.orderStatus !== 'PENDING_PAYMENT') {
            throw new Error('A2M order is not eligible for first fulfillment');
          }
          if (!Number.isFinite(Date.parse(order.payBefore)) || Date.parse(order.payBefore) <= Date.now()) {
            throw new Error('A2M order payment deadline has expired');
          }
          await connection.execute(
            `UPDATE a2m_orders
                SET trade_no = ?, order_status = 'PAID', fulfill_status = 'PENDING_CONFIRM'
              WHERE out_trade_no = ?`,
            [tradeNo, outTradeNo],
          );
        }

        const serviceResult = order.serviceResult || await createResource();
        if (!serviceResult) throw new Error('A2M resource generator returned no result');
        if (!order.serviceResult) {
          await connection.execute(
            `UPDATE a2m_orders SET service_result = ? WHERE out_trade_no = ?`,
            [JSON.stringify(serviceResult), outTradeNo],
          );
        }
        await connection.commit();
        return { state: 'PENDING_CONFIRM', serviceResult };
      } catch (error) {
        await connection.rollback().catch(() => {});
        if (error?.code === 'ER_DUP_ENTRY') throw new Error('A2M trade number has already been used');
        throw error;
      } finally {
        connection.release();
      }
    },

    async markFulfilled(outTradeNo, tradeNo) {
      await ensureSchema();
      const [result] = await getPool().execute(
        `UPDATE a2m_orders
            SET fulfill_status = 'FULFILLED'
          WHERE out_trade_no = ? AND trade_no = ? AND fulfill_status = 'PENDING_CONFIRM'`,
        [outTradeNo, tradeNo],
      );
      if (result.affectedRows > 0) return;

      const order = await this.findByOutTradeNo(outTradeNo);
      if (order?.tradeNo === tradeNo && order.fulfillStatus === 'FULFILLED') return;
      throw new Error('A2M fulfillment state could not be finalized');
    },

    async close() {
      if (pool) await pool.end();
    },
  };
}
