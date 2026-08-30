package constants

// System Codes (3 digits) - 位置 11~13
const (
	SysCodeAcquiringCore  = "010" // 收单核心
	SysCodePaymentCore    = "011" // 支付核心
	SysCodeSettlementCore = "012" // 结算核心
	SysCodeRiskControl    = "013" // 风控服务
	SysCodeUser           = "014" // 用户服务
	SysCodeGateway        = "015" // API 网关
	SysCodeChainCore      = "016" // 链节点代理
	SysCodeMerchantCore   = "017" // 商户核心服务
	SysCodeAccount        = "018" // 账户服务
	SysCodeExchange       = "019" //汇率服务
	SysCodeShopify        = "020" //shopify服务
	SysCodeShoplazza      = "021" //Shoplazza服务
	SysCodeCashier        = "022" //收银核心
	SysCodeAuth           = "023" // Auth 服务
	SysCodeChannelCore    = "024" // 渠道核心
	SysCodeFundProd       = "025" // 提现服务
	SysCodeMerchantPortal = "026" // 商户后台
	SysCodeNotification   = "027" // 通知服务
	SysCodeCommunication  = "028" // 通信服务
	SysCodePayAdmin       = "029" // PayAdmin 后台
	SysCodeReconciliation = "030" // 对账服务
	SysCodeSubscription   = "031" // 订阅服务
	SysCodeAgencyProd     = "032" // 代理商服务
)
