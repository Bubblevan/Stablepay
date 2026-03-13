# Payment Service 模块说明文档

## 架构概述

Payment Service 采用 COLA v5 分层架构，遵循领域驱动设计（DDD）原则。

## 目录结构

- api/ - 接入层 (Adapter)
- internal/ - 内部实现
  - adapter/ - 适配器层实现
  - application/ - 应用层
  - domain/ - 领域层
  - infrastructure/ - 基础设施层
- pkg/ - 公共包
- scripts/ - 数据库脚本

## 支付状态机

CREATED -> PENDING -> CONFIRMED -> COMPLETED
   |          |          |
   v          v          v
 FAILED <- FAILED <- FAILED
   |
   v
CANCELLED

## 核心实体

### Payment (聚合根)

代表一次完整的支付交易生命周期。

### 值对象

- DID: W3C did:solana 标识符
- Amount: 金额（最小单位整数）
- Signature: 支付签名

## 应用服务

### PaymentApplicationService

- InitiatePayment: 发起支付
- GetPaymentStatus: 查询支付状态
- ListPaymentHistory: 查询支付历史
