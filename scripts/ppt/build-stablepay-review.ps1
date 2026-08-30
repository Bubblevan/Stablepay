param(
    [string]$TemplatePath = "D:\下载\Stablepay.pptx",
    [string]$OutputPath = "D:\MyLab\StablePay\outputs\StablePay_项目总结_前半段_审阅版.pptx"
)

$ErrorActionPreference = "Stop"
$LayoutScale = 0.75

function OleColor([int]$r, [int]$g, [int]$b) {
    return $r + ($g * 256) + ($b * 65536)
}

function Pt([single]$value) {
    return [single]($value * $LayoutScale)
}

function AddTextBox {
    param(
        $Slide,
        [single]$Left,
        [single]$Top,
        [single]$Width,
        [single]$Height,
        [string]$Text,
        [int]$FontSize = 18,
        [bool]$Bold = $false,
        [int]$Color = 0,
        [string]$Name = ""
    )

    $shape = $Slide.Shapes.AddTextbox(1, (Pt $Left), (Pt $Top), (Pt $Width), (Pt $Height))
    if ($Name) { $shape.Name = $Name }
    $shape.TextFrame.TextRange.Text = $Text
    $shape.TextFrame.TextRange.Font.NameFarEast = "微软雅黑"
    $shape.TextFrame.TextRange.Font.Name = "Arial"
    $shape.TextFrame.TextRange.Font.Size = $FontSize
    $shape.TextFrame.TextRange.Font.Bold = [int]$Bold
    $shape.TextFrame.TextRange.Font.Color.RGB = $Color
    $shape.TextFrame.MarginLeft = Pt 4
    $shape.TextFrame.MarginRight = Pt 4
    $shape.TextFrame.MarginTop = Pt 2
    $shape.TextFrame.MarginBottom = Pt 2
    $shape.TextFrame.WordWrap = -1
    $shape.TextFrame.AutoSize = 0
    return $shape
}

function AddPanel {
    param(
        $Slide,
        [single]$Left,
        [single]$Top,
        [single]$Width,
        [single]$Height,
        [int]$FillColor,
        [int]$LineColor
    )

    $shape = $Slide.Shapes.AddShape(5, (Pt $Left), (Pt $Top), (Pt $Width), (Pt $Height))
    $shape.Fill.ForeColor.RGB = $FillColor
    $shape.Line.ForeColor.RGB = $LineColor
    $shape.Line.Weight = Pt 1.2
    $shape.Adjustments.Item(1) = 0.08
    return $shape
}

function AddSectionTag {
    param(
        $Slide,
        [single]$Left,
        [single]$Top,
        [single]$Width,
        [single]$Height,
        [string]$Text
    )

    $shape = $Slide.Shapes.AddShape(1, (Pt $Left), (Pt $Top), (Pt $Width), (Pt $Height))
    $shape.Fill.ForeColor.RGB = (OleColor 160 27 16)
    $shape.Line.Visible = 0
    $shape.TextFrame.TextRange.Text = $Text
    $shape.TextFrame.TextRange.Font.NameFarEast = "微软雅黑"
    $shape.TextFrame.TextRange.Font.Size = 16
    $shape.TextFrame.TextRange.Font.Bold = -1
    $shape.TextFrame.TextRange.Font.Color.RGB = (OleColor 255 255 255)
    $shape.TextFrame.TextRange.ParagraphFormat.Alignment = 2
    $shape.TextFrame.VerticalAnchor = 3
    return $shape
}

function AddBulletBlock {
    param(
        $Slide,
        [single]$Left,
        [single]$Top,
        [single]$Width,
        [single]$Height,
        [string]$Header,
        [string[]]$Bullets
    )

    $headerShape = AddTextBox -Slide $Slide -Left $Left -Top $Top -Width $Width -Height 24 -Text $Header -FontSize 20 -Bold $true -Color (OleColor 160 27 16)
    $bodyText = ($Bullets | ForEach-Object { "• $_" }) -join "`r`n"
    $bodyShape = AddTextBox -Slide $Slide -Left $Left -Top ($Top + 34) -Width $Width -Height ($Height - 34) -Text $bodyText -FontSize 18 -Bold $false -Color 0
    $bodyShape.TextFrame.TextRange.ParagraphFormat.Bullet.Visible = 0
    $bodyShape.TextFrame.TextRange.ParagraphFormat.SpaceAfter = 6
    return @($headerShape, $bodyShape)
}

function FillPositionSlide {
    param($Slide)

    AddSectionTag -Slide $Slide -Left 64 -Top 162 -Width 136 -Height 30 -Text "项目定位" | Out-Null
    AddPanel -Slide $Slide -Left 52 -Top 195 -Width 328 -Height 182 -FillColor (OleColor 246 244 242) -LineColor (OleColor 209 209 209) | Out-Null
    AddTextBox -Slide $Slide -Left 74 -Top 220 -Width 286 -Height 128 -Text "为 AI Agent 提供一套可自主发起、可验证、可结算的稳定币支付底座，让支付不再依赖人类身份、银行卡和人工确认流程。" -FontSize 19 -Color 0 | Out-Null

    AddSectionTag -Slide $Slide -Left 458 -Top 162 -Width 150 -Height 30 -Text "第一切入场景" | Out-Null
    AddPanel -Slide $Slide -Left 446 -Top 195 -Width 328 -Height 182 -FillColor (OleColor 246 244 242) -LineColor (OleColor 209 209 209) | Out-Null
    AddTextBox -Slide $Slide -Left 468 -Top 220 -Width 286 -Height 128 -Text "从 ClawHub / OpenClaw Skill 市场切入，解决 Skill 作者希望获得报酬、Agent 需要自动购买工具和能力的问题。" -FontSize 19 -Color 0 | Out-Null

    AddSectionTag -Slide $Slide -Left 836 -Top 162 -Width 146 -Height 30 -Text "生态扩展方向" | Out-Null
    AddPanel -Slide $Slide -Left 824 -Top 195 -Width 328 -Height 182 -FillColor (OleColor 246 244 242) -LineColor (OleColor 209 209 209) | Out-Null
    AddTextBox -Slide $Slide -Left 846 -Top 220 -Width 286 -Height 128 -Text "双轨支持 OpenClaw 插件与 MCP SDK，后端从服务单一宿主升级为服务整个 Agent 生态。" -FontSize 19 -Color 0 | Out-Null

    AddTextBox -Slide $Slide -Left 80 -Top 420 -Width 1120 -Height 108 -Text "我们的目标不是再做一个收银台，而是把 AI 如何支付 抽象成一套底层逻辑：谁可以付、何时可以付、付完如何验证与复用。" -FontSize 20 -Bold $true -Color (OleColor 20 74 126) | Out-Null

    AddTextBox -Slide $Slide -Left 90 -Top 548 -Width 1080 -Height 74 -Text "一句话总结：StablePay 希望成为 Web3 时代面向 AI Agent 的 PayPal 级支付基础设施。" -FontSize 20 -Bold $true -Color (OleColor 160 27 16) | Out-Null
}

function FillServiceSlide {
    param(
        $Slide,
        [string]$RoleText,
        [string[]]$LeftBullets,
        [string[]]$RightBullets
    )

    AddSectionTag -Slide $Slide -Left 64 -Top 156 -Width 112 -Height 30 -Text "服务职责" | Out-Null
    AddTextBox -Slide $Slide -Left 64 -Top 198 -Width 1090 -Height 80 -Text $RoleText -FontSize 18 -Bold $true -Color 0 | Out-Null

    AddPanel -Slide $Slide -Left 58 -Top 286 -Width 515 -Height 300 -FillColor (OleColor 248 247 245) -LineColor (OleColor 217 217 217) | Out-Null
    AddPanel -Slide $Slide -Left 622 -Top 286 -Width 515 -Height 300 -FillColor (OleColor 248 247 245) -LineColor (OleColor 217 217 217) | Out-Null

    AddBulletBlock -Slide $Slide -Left 86 -Top 308 -Width 462 -Height 252 -Header "关键能力" -Bullets $LeftBullets | Out-Null
    AddBulletBlock -Slide $Slide -Left 650 -Top 308 -Width 462 -Height 252 -Header "设计亮点" -Bullets $RightBullets | Out-Null
}

if (-not (Test-Path -LiteralPath $TemplatePath)) {
    throw "Template not found: $TemplatePath"
}

$outputDir = Split-Path -Parent $OutputPath
if (-not (Test-Path -LiteralPath $outputDir)) {
    New-Item -ItemType Directory -Path $outputDir | Out-Null
}

Copy-Item -LiteralPath $TemplatePath -Destination $OutputPath -Force

$ppt = $null
$presentation = $null

try {
    $ppt = New-Object -ComObject PowerPoint.Application
    $presentation = $ppt.Presentations.Open($OutputPath, $false, $false, $false)

    FillPositionSlide -Slide $presentation.Slides.Item(5)

    FillServiceSlide -Slide $presentation.Slides.Item(9) `
        -RoleText "API Gateway 是整个 StablePay 后端的统一接入入口，负责把所有外部请求安全、稳定地转发到具体微服务。" `
        -LeftBullets @(
            "统一承接 /api/v1/{did,payment,verify,query,revenue,sales} 21 条核心路由",
            "按路径和方法分发请求，让客户端始终只面对一个入口",
            "将鉴权、限流、防重放等横切逻辑集中治理，避免下游服务重复实现"
        ) `
        -RightBullets @(
            "中间件拆成 Globals + Per-route 两段式，既统一又可按路由差异化配置",
            "三轴限流同时覆盖 IP / DID / Route，命中后可精确告诉前端哪里超限",
            "Redis 不可用时自动回退到内存 nonce store 与 limiter，保证服务不挂"
        )

    FillServiceSlide -Slide $presentation.Slides.Item(10) `
        -RoleText "DID Service 为 AI Agent 生成并管理去中心化身份，是整套系统最底层、最基础的身份能力提供者。" `
        -LeftBullets @(
            "生成 Ed25519 密钥对并拼装 did:solana:<pubkey> 身份标识",
            "提供身份注册、查询、验签、状态管理等基础能力",
            "所有支付请求都可先由 DID 服务完成签名校验"
        ) `
        -RightBullets @(
            "自身不依赖其他业务服务，避免微服务循环依赖",
            "签名内容强制绑定 timestamp + nonce，抵御重放攻击",
            "身份状态机支持 active / disabled，便于风控与封禁"
        )

    FillServiceSlide -Slide $presentation.Slides.Item(11) `
        -RoleText "Payment Service 是支付链路核心，负责处理 x402 协议、协调链上转账，并保证交易可重试、不可重复扣款。" `
        -LeftBullets @(
            "处理 HTTP 402 Payment Required 的支付请求与状态流转",
            "同步调用 DID 验签、调用 Blockchain Adapter 执行链上结算",
            "支付成功后发布 payment_events，驱动后续验证与记账"
        ) `
        -RightBullets @(
            "基于 COLA 分层架构组织业务，清晰拆分 domain / application / infrastructure",
            "幂等键 + Nonce + 数据库约束三重防护，避免重复支付",
            "采用同步支付执行 + 异步购买记录落库的最终一致性设计"
        )

    FillServiceSlide -Slide $presentation.Slides.Item(12) `
        -RoleText "Blockchain Adapter 把业务支付语义翻译成 Solana 链上动作，是系统与真实稳定币网络交互的执行层。" `
        -LeftBullets @(
            "对接 Solana 与 USDC，负责余额查询、转账构建、交易状态确认",
            "支持客户端部分签名 + 热钱包补 fee payer，以及纯托管转账双模式",
            "根据补贴比例计算平台需要承担的 SOL Gas 成本"
        ) `
        -RightBullets @(
            "通过接口倒置把链上 RPC、交易构建、热钱包签名全部抽象成 gateway",
            "三阶段 base64 交易审计日志可精确定位 fee payer、source ATA、dest ATA",
            "出问题时能直接从日志反查到具体交易构建阶段，便于审计和排障"
        )

    FillServiceSlide -Slide $presentation.Slides.Item(13) `
        -RoleText "Verification Service 负责把链上支付转化为平台已购买的可信证明，同时承担 X 验证与新用户激励发放。" `
        -LeftBullets @(
            "订阅 payment_events，把 agent 与 skill 的购买关系写入 purchase_records",
            "对外提供单次验证、批量验证、购买证明查询等 RPC 能力",
            "实现 X 推文绑定验证与 1 USDC onboarding 奖励发放"
        ) `
        -RightBullets @(
            "写库失败返回 ConsumeRetryLater，让 RocketMQ 自动重投",
            "限制同一 X 账号不可重复绑定不同 DID，防止薅奖励",
            "奖励单号采用 x-reward-<tweetID>-<timestamp>，天然带可溯源信息"
        )

    FillServiceSlide -Slide $presentation.Slides.Item(14) `
        -RoleText "Query Service 面向用户和商家提供余额、交易、收益与销售查询，是支付能力产品化后的可视化出口。" `
        -LeftBullets @(
            "提供 GetBalanceSummary / ListTransactions / GetRevenueSummary / ListSales",
            "同时服务买家侧余额查询和商家侧收益、销售明细查询",
            "支持内部交易同步接口，作为 MQ 消费后的幂等写入入口"
        ) `
        -RightBullets @(
            "链上余额与本地账本交叉验证，既看得见链上资金，也看得见业务流水",
            "链上查询失败时余额字段自动降级为 0，但不阻断其他查询接口",
            "趋势统计、分页查询、结构化日志已经具备进一步产品化基础"
        )

    for ($i = $presentation.Slides.Count; $i -ge 15; $i--) {
        $presentation.Slides.Item($i).Delete()
    }

    $presentation.Save()
    Write-Output $OutputPath
}
finally {
    if ($presentation -ne $null) {
        $presentation.Close()
        [void][System.Runtime.InteropServices.Marshal]::FinalReleaseComObject($presentation)
    }
    if ($ppt -ne $null) {
        $ppt.Quit()
        [void][System.Runtime.InteropServices.Marshal]::FinalReleaseComObject($ppt)
    }
}
