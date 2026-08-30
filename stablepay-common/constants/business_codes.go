package constants

// Business Codes (2 digits) - 位置 14~15
// 由各域内部定，如 01 支付，02 预授权，03 退款等
const (
	BizCodeGeneral = "00" // 通用
	BizCodePayment = "01" // 支付
	BizCodePreAuth = "02" // 预授权
	BizCodeRefund  = "03" // 退款

	// AccountCore 业务码
	BizCodeAccount = "01" // 账户管理
	BizCodeBalance = "02" // 余额操作
	BizCodeTxn     = "03" // 交易流水
	// 注意：BizCodeWithdraw (03) 已废弃，提现业务将迁移至 FundProd 服务管理
	// AccountCore 中 app/withdrawal 模块的监控埋点暂时保留，使用 FundProd 的业务码
	// 迁移完成后，该模块将从 AccountCore 中移除

	// SettlePlatform 业务码
	BizCodeSettlement = "01" // 结算处理
	BizCodeRecon      = "02" // 对账服务
	BizCodeLiquidity  = "03" // 流动性管理
	BizCodeCharging   = "04" // 计费服务
	BizCodeSweep      = "05" // 资金归集
	BizCodeDataSync   = "06" // 数据同步

	// FundProd 业务码
	BizCodeWithdrawRequest = "01" // 提现申请
	BizCodeWithdrawReview  = "02" // 提现审核
	BizCodeTransfer        = "03" // 链上转账
	BizCodeAddress         = "04" // 地址管理

	// Subscription 业务码
	BizCodeSubscription     = "01" // 订阅管理
	BizCodeSubscriptionItem = "02" // 订阅项管理
	BizCodeInvoice          = "03" // 账单管理

	// Notification 服务使用通用业务码 BizCodeGeneral
	// 原因：Notification 是基础设施服务，不产生业务，只传递其他服务的业务事件
	// 具体业务事件类型通过日志中的 event_type 字段体现（如 payment.completed, refund.success）
)
