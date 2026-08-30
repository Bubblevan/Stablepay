package constants

// Standard Reason Codes
// 9-digit error code structure: SysCode(3) + BizCode(2) + ReasonCode(4)
// These constants define the last 4 digits (ReasonCode).
const (
	// 0xxx: Success / Parameter Validation
	ReasonSuccess       = "0000"
	ReasonInvalidParams = "0001" // 参数格式错误
	ReasonMissingParams = "0002" // 缺少必选参数
	ReasonSignatureError = "0003" // 签名错误

	// 1xxx: Processing / Intermediate States
	ReasonProcessing     = "1000"
	ReasonPendingConfirm = "1001" // Waiting for block confirmations
	ReasonBroadcasting   = "1002" // In mempool

	// 2xxx: Business Logic Failures (Terminal)
	ReasonInsufficientFunds     = "2001"
	ReasonInvalidAddress        = "2002"
	ReasonRiskControl           = "2003"
	ReasonLimitExceeded         = "2004"
	ReasonContractReverted      = "2005" // Smart contract execution failed
	ReasonGasLimitExceeded      = "2006"
	ReasonNonceTooLow           = "2007"
	ReasonInsufficientAllowance = "2008" // ERC20 Allowance insufficient
	ReasonNotFound              = "2101" // 资源不存在
	ReasonAlreadyExists         = "2102" // 资源已存在
	ReasonInvalidStatus         = "2103" // 状态不合法
	ReasonForbidden             = "2104" // 操作被禁止
	ReasonValidationFailed      = "2105" // 业务校验失败

	// 3xxx: Upstream/Network Errors (Retryable/Unknown)
	ReasonUpstreamTimeout    = "3001"
	ReasonRPCTimeout         = "3002" // Blockchain RPC timeout
	ReasonNetworkCongestion  = "3003" // Gas price too high / Congestion
	ReasonBlockReorg         = "3004" // Chain reorg detected
	ReasonTransactionDropped = "3005" // Dropped from mempool
	ReasonDatabaseError      = "3101" // 数据库错误
	ReasonRedisError         = "3102" // Redis 错误
	ReasonRPCError           = "3103" // RPC 调用失败
	ReasonMQError            = "3104" // 消息队列错误

	// 4xxx: Configuration Errors
	ReasonConfigMissing = "4001" // 配置缺失
	ReasonEnvError      = "4002" // 环境变量错误

	// 9xxx: System Errors
	ReasonSystemError = "9999"
)
