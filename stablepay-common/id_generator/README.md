# Unified ID Generator (通用ID生成器)

本模块提供了基于MySQL Segment模式（Leaf-Segment算法）的高性能分布式ID生成器。

包含两类ID生成策略：
1. **Serial (连续型)**: 适用于支付订单号等需要携带业务信息和日期的场景。
2. **Ident (随机型)**: 适用于用户ID (UID)、商户ID (PID) 等需要防枚举和混淆的场景。

## 目录
- [接入指南](#接入指南)
- [技术原理](#技术原理)
- [配置说明](#配置说明)

## 接入指南

### 1. 基础依赖 (DB)
```go
import (
    "gorm.io/driver/mysql"
    "gorm.io/gorm"
    "code.wenfu.cn/stablepay/stablepay-common/id_generator/core"
    "code.wenfu.cn/stablepay/stablepay-common/id_generator/serial"
    "code.wenfu.cn/stablepay/stablepay-common/id_generator/ident"
)

// 初始化数据库连接
db, _ := gorm.Open(mysql.Open("dsn..."), &gorm.Config{})

// 初始化 Allocator (对应数据库中的 biz_tag)
orderAllocator := core.NewSegmentAllocator(db, "payment_order_std")
userAllocator := core.NewSegmentAllocator(db, "user_id")
```

### 2. 生成 32位 支付订单号 (Serial)
格式：`yyyyMMdd` + `DataVer(1)` + `SysVer(1)` + `SysCode(3)` + `BizCode(2)` + `DC(2)` + `DB(2)` + `TB(2)` + `Env(1)` + `Reserved(2)` + `Seq(8)`

```go
config := serial.StandardConfig{
    DataVer:  "1",
    SysVer:   "1",
    SysCode:  "010", // 收单核心
    BizCode:  "01",  // 支付
    DC:       "01",  // 机房
    Env:      "1",   // 生产
    Reserved: "00",
}

gen, _ := serial.NewGenerator(config, orderAllocator)

// 传入 shardingKey (如 uid) 用于计算分库分表位
id, err := gen.Generate(ctx, 123456789)
// output: 202310271101001010123891000000001
```

### 3. 生成 16位 用户/商户ID (Ident)
格式：`Prefix(4)` + `ObfuscatedSeq(11)` + `Check(1)`

```go
// 使用默认配置生成 UID
uidGen, _ := ident.NewGenerator(ident.DefaultUserConfig, userAllocator)
uid, err := uidGen.Generate(ctx)

// 使用默认配置生成 PID
pidGen, _ := ident.NewGenerator(ident.DefaultMerchantConfig, userAllocator)
pid, err := pidGen.Generate(ctx)
```

## 数据库表结构

```sql
CREATE TABLE `leaf_alloc` (
  `biz_tag` varchar(128)  NOT NULL DEFAULT '',
  `max_id` bigint(20) NOT NULL DEFAULT '1',
  `step` int(11) NOT NULL,
  `description` varchar(256)  DEFAULT NULL,
  `update_time` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`biz_tag`)
) ENGINE=InnoDB;

INSERT INTO leaf_alloc (biz_tag, max_id, step, description) VALUES ('payment_order_std', 1, 2000, 'Standard Payment Order ID');
INSERT INTO leaf_alloc (biz_tag, max_id, step, description) VALUES ('user_id', 1, 2000, 'User ID / Merchant ID Shared');
```
