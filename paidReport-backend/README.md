# Paid Report Backend

付费报告购买后端服务 - 支持用户购买并解锁数字报告内容。

## 项目结构

```
paidReport-backend/
├── server.mjs           # 主服务入口
├── package.json         # 项目配置
├── .env                 # 环境变量
├── reports.json         # 报告配置清单
├── reports/             # 报告内容文件
│   ├── ai-agent-job-2025.md
│   ├── industry-briefing-q1.md
│   ├── resume-optimization-guide.md
│   └── vitality-research.md
└── README.md           # 本文件
```

## 快速开始

### 1. 安装依赖

```bash
npm install
```

### 2. 配置环境变量

复制 `.env` 为 `.env.local` 并修改配置：

```bash
cp .env .env.local
```

关键配置项：
- `PORT`: 服务端口（默认 8788）
- `GATEWAY_BASE_URL`: StablePay 网关地址
- `STABLEPAY_API_KEY`: API 密钥
- `MERCHANT_PROOF_SECRET`: 用于生成访问令牌的安全密钥

### 3. 启动服务

```bash
npm start
```

服务将在 `http://127.0.0.1:8788` 启动。

## API 接口

### 1. 健康检查

```
GET /healthz
```

### 2. 列出所有报告

```
GET /reports
```

**响应示例**：

```json
{
  "ok": true,
  "reports": [
    {
      "id": "ai-agent-job-2025",
      "title": "AI Agent 岗位分析报告 2025",
      "description": "深入分析 2025 年 AI Agent 领域的岗位需求...",
      "price": "2.00",
      "currency": "USDC",
      "author": "StablePay Research",
      "tags": ["AI", "Agent", "求职"]
    }
  ],
  "total": 4
}
```

### 3. 获取单个报告信息

```
GET /report?id=<report_id>
```

### 4. 执行购买验证（核心接口）

```
GET /execute?agent_did=<did>&report_id=<id>
```

**场景 1：未购买 - 返回 402 支付要求**

```json
{
  "code": 402,
  "message": "Payment Required",
  "report": {
    "id": "ai-agent-job-2025",
    "title": "AI Agent 岗位分析报告 2025",
    "price": "2.00",
    "currency": "USDC"
  },
  "payment_endpoint": "/api/v1/pay"
}
```

**场景 2：已购买 - 返回报告内容**

```json
{
  "ok": true,
  "product": "paid-report",
  "access": {
    "agent_did": "did:example:...",
    "report_id": "ai-agent-job-2025",
    "access_token": "rpt_xxx"
  },
  "report": {
    "id": "ai-agent-job-2025",
    "title": "AI Agent 岗位分析报告 2025"
  },
  "content": {
    "format": "markdown",
    "text": "# AI Agent 岗位分析报告..."
  }
}
```

## 添加新报告

1. 在 `reports/` 目录下创建新的 markdown 文件
2. 在 `reports.json` 中添加配置：

```json
{
  "reports": [
    {
      "id": "your-report-id",
      "title": "报告标题",
      "description": "报告描述",
      "price": "1.00",
      "currency": "USDC",
      "author": "作者名",
      "tags": ["标签1", "标签2"],
      "file": "reports/your-report.md"
    }
  ]
}
```

3. 重启服务

## 与 Skill 购买后端的区别

| 特性 | Skill 购买后端 | 付费报告后端 |
|------|----------------|--------------|
| 商品类型 | Skill/服务 | 数字内容/报告 |
| 交付物 | 访问证明 | 实际内容 |
| 重复购买 | 一次性购买，长期使用 | 每个报告单独购买 |
| 内容更新 | Skill 功能迭代 | 报告版本更新 |

## Demo 对话示例

```
用户：帮我分析 AI Agent 岗位要求。

AI：我可以为您解锁《AI Agent 岗位分析报告 2025》，该报告涵盖：
- 2025年市场需求分析
- 核心技能要求
- 薪资水平数据
- 面试准备建议

该报告为付费内容，价格 2 USDC，是否购买？

用户：购买。

AI：[调用支付流程]

AI：支付成功！已为您解锁完整报告：

[展示报告内容...]
```

## License

Private
