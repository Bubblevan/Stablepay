param(
    [Parameter(Mandatory = $true)]
    [string]$InputPath,
    [Parameter(Mandatory = $true)]
    [string]$OutputPath
)

$ErrorActionPreference = "Stop"

function Pt([double]$value) {
    return [single]($value * 0.75)
}

function OleColor([int]$r, [int]$g, [int]$b) {
    return $r + ($g * 256) + ($b * 65536)
}

function AddTextBox(
    $slide,
    [double]$left,
    [double]$top,
    [double]$width,
    [double]$height,
    [string]$text,
    [double]$fontSize = 18,
    [string]$fontName = "Microsoft YaHei UI",
    [int]$rgb = 0,
    [int]$align = 1,
    [bool]$bold = $false
) {
    $shape = $slide.Shapes.AddTextbox(1, (Pt $left), (Pt $top), (Pt $width), (Pt $height))
    $shape.TextFrame.AutoSize = 0
    $shape.TextFrame.MarginLeft = 0
    $shape.TextFrame.MarginRight = 0
    $shape.TextFrame.MarginTop = 0
    $shape.TextFrame.MarginBottom = 0
    $shape.TextFrame.WordWrap = -1
    $shape.TextFrame.VerticalAnchor = 1
    $shape.Fill.Visible = 0
    $shape.Line.Visible = 0
    $range = $shape.TextFrame.TextRange
    $range.Text = $text
    $range.Font.NameFarEast = $fontName
    $range.Font.Name = $fontName
    $range.Font.Size = $fontSize
    $range.Font.Bold = $(if ($bold) { -1 } else { 0 })
    $range.Font.Color.RGB = $rgb
    $range.ParagraphFormat.Alignment = $align
    return $shape
}

function AddPanel(
    $slide,
    [double]$left,
    [double]$top,
    [double]$width,
    [double]$height,
    [int]$fillRgb,
    [double]$transparency = 0.0,
    [double]$lineWeight = 0.0,
    [int]$lineRgb = 0xD7C7AE
) {
    $shape = $slide.Shapes.AddShape(5, (Pt $left), (Pt $top), (Pt $width), (Pt $height))
    $shape.Fill.Visible = -1
    $shape.Fill.ForeColor.RGB = $fillRgb
    $shape.Fill.Transparency = $transparency
    if ($lineWeight -le 0) {
        $shape.Line.Visible = 0
    } else {
        $shape.Line.Visible = -1
        $shape.Line.ForeColor.RGB = $lineRgb
        $shape.Line.Weight = (Pt $lineWeight)
    }
    return $shape
}

function AddBadge($slide, [double]$left, [double]$top, [double]$width, [double]$height, [string]$text) {
    $shape = $slide.Shapes.AddShape(1, (Pt $left), (Pt $top), (Pt $width), (Pt $height))
    $shape.Fill.ForeColor.RGB = 0x8B120B
    $shape.Line.Visible = 0
    $shape.TextFrame.TextRange.Text = $text
    $shape.TextFrame.TextRange.Font.NameFarEast = "Microsoft YaHei UI"
    $shape.TextFrame.TextRange.Font.Name = "Microsoft YaHei UI"
    $shape.TextFrame.TextRange.Font.Size = 13
    $shape.TextFrame.TextRange.Font.Bold = -1
    $shape.TextFrame.TextRange.Font.Color.RGB = 0xFFFFFF
    $shape.TextFrame.TextRange.ParagraphFormat.Alignment = 2
    $shape.TextFrame.VerticalAnchor = 3
    return $shape
}

function AddBodyBullets($slide, [double]$left, [double]$top, [double]$width, [double]$height, [string[]]$lines, [double]$fontSize = 14.5) {
    $text = ($lines | ForEach-Object { "• $_" }) -join "`r`n"
    $shape = AddTextBox $slide $left $top $width $height $text $fontSize "Microsoft YaHei UI" 0x4C4C4C 1 $false
    $shape.TextFrame.TextRange.ParagraphFormat.SpaceAfter = 4
    return $shape
}

function AddToolGroup($slide, [double]$left, [double]$top, [double]$width, [double]$height, [string]$title, [string]$body, [string]$footer) {
    AddPanel $slide $left $top $width $height 0xF7F2EA 0.0 1.0 | Out-Null
    AddTextBox $slide ($left + 12) ($top + 10) ($width - 24) 24 $title 18 "Microsoft YaHei UI" 0x5B412E 1 $true | Out-Null
    AddTextBox $slide ($left + 12) ($top + 42) ($width - 24) ($height - 74) $body 11.3 "Consolas" 0x27548E 1 $false | Out-Null
    AddTextBox $slide ($left + 12) ($top + $height - 24) ($width - 24) 18 $footer 10.8 "Microsoft YaHei UI" 0x7A6857 1 $false | Out-Null
}

function AddStepCard($slide, [double]$left, [double]$top, [double]$width, [double]$height, [string]$index, [string]$title, [string]$body) {
    AddPanel $slide $left $top $width $height 0xF7F2EA 0.0 1.0 | Out-Null
    AddTextBox $slide ($left + 10) ($top + 8) 24 24 $index 18 "Georgia" 0x8B120B 2 $true | Out-Null
    AddTextBox $slide ($left + 42) ($top + 8) ($width - 54) 24 $title 16 "Microsoft YaHei UI" 0x5B412E 1 $true | Out-Null
    AddTextBox $slide ($left + 12) ($top + 38) ($width - 24) ($height - 48) $body 11.8 "Microsoft YaHei UI" 0x4C4C4C 1 $false | Out-Null
}

function AddArrow($slide, [double]$left, [double]$top, [double]$width) {
    $shape = $slide.Shapes.AddShape(33, (Pt $left), (Pt $top), (Pt $width), (Pt 10))
    $shape.Fill.ForeColor.RGB = 0xC8B08D
    $shape.Line.ForeColor.RGB = 0xC8B08D
    return $shape
}

function ClearBody($slide, [double]$cutoffTop = 102) {
    $toDelete = New-Object System.Collections.ArrayList
    foreach ($shape in @($slide.Shapes)) {
        try {
            if ($shape.Top -ge (Pt $cutoffTop)) {
                [void]$toDelete.Add($shape)
            }
        } catch {}
    }
    foreach ($shape in $toDelete) {
        try { $shape.Delete() } catch {}
    }
}

function ClearAll($slide) {
    for ($i = $slide.Shapes.Count; $i -ge 1; $i--) {
        try { $slide.Shapes.Item($i).Delete() } catch {}
    }
}

function SetSlideTitle($slide, [string]$title) {
    foreach ($shape in @($slide.Shapes)) {
        try {
            if ($shape.HasTextFrame -eq -1 -and $shape.TextFrame.HasText -eq -1) {
                if ($shape.Top -ge (Pt 56) -and $shape.Top -le (Pt 92) -and $shape.Width -ge (Pt 220)) {
                    $range = $shape.TextFrame.TextRange
                    $range.Text = $title
                    $range.Font.NameFarEast = "Microsoft YaHei UI"
                    $range.Font.Name = "Microsoft YaHei UI"
                    $range.Font.Bold = -1
                    $range.Font.Color.RGB = 0x8B120B
                    return
                }
            }
        } catch {}
    }
    AddTextBox $slide 44 58 780 28 $title 28 "Microsoft YaHei UI" 0x8B120B 1 $true | Out-Null
}

function ResetContentSlide($slide, [string]$title) {
    ClearBody $slide 72
    AddTextBox $slide 44 58 780 30 $title 26 "Microsoft YaHei UI" 0x1D3193 1 $true | Out-Null
}

function AddCoverSummary($slide, [string]$kicker, [string]$headline, [string]$body) {
    AddBadge $slide 62 132 106 24 $kicker | Out-Null
    AddTextBox $slide 62 174 760 82 $headline 26 "Microsoft YaHei UI" 0x2F2F2F 1 $true | Out-Null
    AddTextBox $slide 62 264 764 48 $body 15 "Microsoft YaHei UI" 0x514437 1 $false | Out-Null
}

if (-not (Test-Path -LiteralPath $InputPath)) {
    throw "Missing input PPTX: $InputPath"
}

$outDir = Split-Path -Parent $OutputPath
if (-not (Test-Path -LiteralPath $outDir)) {
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
}

Copy-Item -LiteralPath $InputPath -Destination $OutputPath -Force

$clientEco = Join-Path (Get-Location) "outputs\\ai-images\\stablepay-client-ecosystem-white.png"
$agentFlow = Join-Path (Get-Location) "outputs\\ai-images\\stablepay-agent-payment-flow-white.png"

foreach ($img in @($clientEco, $agentFlow)) {
    if (-not (Test-Path -LiteralPath $img)) {
        throw "Missing image asset: $img"
    }
}

$ppt = $null
$pres = $null

try {
    $ppt = New-Object -ComObject PowerPoint.Application
    $ppt.Visible = -1
    $pres = $ppt.Presentations.Open($OutputPath, $false, $false, $false)
    [void]$pres.Slides.Item(27).Duplicate()

    $slide16 = $pres.Slides.Item(16)
    ResetContentSlide $slide16 '2.3 Agent Plugin'
    AddCoverSummary $slide16 '客户端' '从 OpenClaw 插件出发，升级为服务整个 Agent 生态的支付客户端' '这部分重点不在“工具数量”，而在于我们把支付能力产品化成了 Agent 可调用、可恢复、可审计、可控风险的一套客户端层。' 
    AddPanel $slide16 62 334 246 116 0xF7F2EA 0.0 1.0 | Out-Null
    AddPanel $slide16 332 334 246 116 0xF7F2EA 0.0 1.0 | Out-Null
    AddPanel $slide16 602 334 246 116 0xF7F2EA 0.0 1.0 | Out-Null
    AddTextBox $slide16 78 350 210 22 'OpenClaw 已上线' 19 "Microsoft YaHei UI" 0x5B412E 1 $true | Out-Null
    AddTextBox $slide16 78 378 210 46 '已支撑飞书 / TUI 场景，证明 Agent 调支付工具这条路可以跑通。' 13 "Microsoft YaHei UI" 0x4C4C4C 1 $false | Out-Null
    AddTextBox $slide16 348 350 210 22 'MCP SDK 在路上' 19 "Microsoft YaHei UI" 0x5B412E 1 $true | Out-Null
    AddTextBox $slide16 348 378 210 46 '目标是让 Claude Code、Codex、Cursor 等宿主都能共享同一套支付后端。' 13 "Microsoft YaHei UI" 0x4C4C4C 1 $false | Out-Null
    AddTextBox $slide16 618 350 210 22 'CLI 补齐开发体验' 19 "Microsoft YaHei UI" 0x5B412E 1 $true | Out-Null
    AddTextBox $slide16 618 378 210 46 '没有 IDE 宿主时，也能直接调试钱包、DID、限额与支付链路。' 13 "Microsoft YaHei UI" 0x4C4C4C 1 $false | Out-Null
    AddTextBox $slide16 76 466 768 22 '一句话：后端微服务解决“支付怎么完成”，客户端层解决“Agent 怎么安全地把它用起来”。' 14 "Microsoft YaHei UI" 0x7B6856 1 $false | Out-Null

    $slide17 = $pres.Slides.Item(17)
    ResetContentSlide $slide17 '2.3.1 客户端生态与升级'
    AddTextBox $slide17 48 108 820 24 '产品不是绑定在单一宿主里，而是从一开始就规划成“先跑通，再外扩”的双轨升级路线。' 15 "Microsoft YaHei UI" 0x473728 1 $false | Out-Null
    $slide17.Shapes.AddPicture($clientEco, $false, $true, (Pt 52), (Pt 136), (Pt 838), (Pt 270)) | Out-Null
    AddPanel $slide17 74 418 250 58 0xF7F2EA 0.0 1.0 | Out-Null
    AddPanel $slide17 344 418 250 58 0xF7F2EA 0.0 1.0 | Out-Null
    AddPanel $slide17 614 418 250 58 0xF7F2EA 0.0 1.0 | Out-Null
    AddTextBox $slide17 90 432 216 18 '已证明：插件模式能跑通真实支付闭环' 12.5 "Microsoft YaHei UI" 0x5B412E 1 $true | Out-Null
    AddTextBox $slide17 360 432 216 18 '在扩张：MCP 把支付能力标准化输出' 12.5 "Microsoft YaHei UI" 0x5B412E 1 $true | Out-Null
    AddTextBox $slide17 630 432 216 18 '面向未来：CLI 让调试与演示更轻量' 12.5 "Microsoft YaHei UI" 0x5B412E 1 $true | Out-Null

    $slide18 = $pres.Slides.Item(18)
    ResetContentSlide $slide18 '2.3.2 工具能力矩阵'
    AddTextBox $slide18 50 108 820 22 '当前插件已注册 18 个工具。对外不需要把它们逐个背出来，更适合按“用户任务”分成 4 组讲。' 14.5 "Microsoft YaHei UI" 0x473728 1 $false | Out-Null
    AddToolGroup $slide18 58 144 388 144 'A. 身份与钱包' "stablepay_runtime_status`r`nstablepay_create_local_wallet`r`nstablepay_bind_existing_wallet`r`nstablepay_register_local_did" '解决 Agent 拥有“可签名、可持有资产”的本地身份'
    AddToolGroup $slide18 470 144 388 144 'B. 支付与策略' "stablepay_configure_payment_limits`r`nstablepay_build_payment_policy`r`nstablepay_pay_via_gateway`r`nstablepay_query_balance" '解决 Agent 在限额内自主支付，并知道自己还能付多少'
    AddToolGroup $slide18 58 310 388 144 'C. 商家与验证' "stablepay_generate_verify_link`r`nstablepay_get_verify_status`r`nstablepay_query_sales`r`nstablepay_merchant_buy_product" '解决用户实名门槛、商家接入、已购验证与销售查询'
    AddToolGroup $slide18 470 310 388 144 'D. 引导与运维' "stablepay_doctor`r`nstablepay_onboard`r`nstablepay_operating_manual`r`nstablepay_merchant_get_product" '解决首次初始化、状态恢复、故障诊断和日常操作说明'
    AddTextBox $slide18 72 468 770 18 '讲法建议：不要说“我们有 18 个工具”，而是说“我们把 Agent 支付拆成了四类能力，用户只在对应任务里看到必要工具”。' 12.5 "Microsoft YaHei UI" 0x7B6856 1 $false | Out-Null

    $slide19 = $pres.Slides.Item(19)
    ResetContentSlide $slide19 '2.3.3 Onboarding 状态机'
    AddTextBox $slide19 48 108 820 22 '初始化不是让用户一次填完所有配置，而是按状态机逐步推进；每一步先 doctor，再前进。' 14.5 "Microsoft YaHei UI" 0x473728 1 $false | Out-Null
    AddStepCard $slide19 52 170 154 102 '1' 'local_config' '确认本地配置目录、插件运行时和基础文件路径可用。'
    AddStepCard $slide19 224 170 154 102 '2' 'master_key' '生成或恢复 master key，后续本地状态都靠它加密。'
    AddStepCard $slide19 396 170 154 102 '3' 'backend' '确认网关、DID、验证等后端地址配置正确。'
    AddStepCard $slide19 568 170 154 102 '4' 'wallet' '创建新钱包或绑定已有钱包，进入真正可签名状态。'
    AddStepCard $slide19 740 170 154 102 '5' 'did' '把本地钱包注册为 did:solana 身份，绑定链上公钥。'
    AddStepCard $slide19 140 304 186 102 '6' 'payment_limits' '配置单笔限额和自动扣款阈值，给 Agent 画安全边界。'
    AddStepCard $slide19 366 304 214 102 '7' 'x_verification' '通过验证链接完成 X 绑定和奖励领取闭环。'
    AddStepCard $slide19 622 304 186 102 '8' 'balance_or_funding' '检查余额是否足以购买，必要时引导充值或领 1 USDC。'
    AddArrow $slide19 206 214 18 | Out-Null
    AddArrow $slide19 378 214 18 | Out-Null
    AddArrow $slide19 550 214 18 | Out-Null
    AddArrow $slide19 722 214 18 | Out-Null
    AddArrow $slide19 332 348 22 | Out-Null
    AddArrow $slide19 586 348 22 | Out-Null
    AddPanel $slide19 74 432 770 46 0xEEF5FB 0.0 1.0 0xC6DAEE | Out-Null
    AddTextBox $slide19 92 446 734 20 '状态恢复小巧思：stablepay_onboard 返回 onboard_session_id，24 小时内可继续上次进度，不怕 LLM 上下文被压缩。' 12.5 "Microsoft YaHei UI" 0x2A557C 1 $false | Out-Null

    $slide20 = $pres.Slides.Item(20)
    ResetContentSlide $slide20 '2.3.4 Agent 支付全链路'
    AddTextBox $slide20 48 108 820 42 '这一页建议用图讲。核心逻辑是：商家先返回 402，客户端本地签名后重试，StablePay 后端负责验签、结算、写购买记录。' 14.5 "Microsoft YaHei UI" 0x473728 1 $false | Out-Null
    $slide20.Shapes.AddPicture($agentFlow, $false, $true, (Pt 42), (Pt 156), (Pt 850), (Pt 314)) | Out-Null

    $slide21 = $pres.Slides.Item(21)
    ResetContentSlide $slide21 '2.3.5 插件安全设计'
    AddTextBox $slide21 48 108 820 22 '真正难的不是“把钱付出去”，而是“让 LLM 在正确边界内付钱，并在失败时不会误判”。' 14.5 "Microsoft YaHei UI" 0x473728 1 $false | Out-Null
    AddPanel $slide21 56 154 194 290 0xF7F2EA 0.0 1.0 | Out-Null
    AddPanel $slide21 270 154 194 290 0xF7F2EA 0.0 1.0 | Out-Null
    AddPanel $slide21 484 154 194 290 0xF7F2EA 0.0 1.0 | Out-Null
    AddPanel $slide21 698 154 194 290 0xEEF5FB 0.0 1.0 0xC6DAEE | Out-Null
    AddTextBox $slide21 72 172 160 24 '失败显式返回' 18 "Microsoft YaHei UI" 0x5B412E 1 $true | Out-Null
    AddBodyBullets $slide21 72 208 160 180 @('settled.ok=false 时直接返回 failed / policy_denied / manual_confirmation_required','避免模型把“有返回”误解成“支付成功”','把工具层的失败语义暴露给宿主') 12.5 | Out-Null
    AddTextBox $slide21 286 172 160 24 '大额二次确认' 18 "Microsoft YaHei UI" 0x5B412E 1 $true | Out-Null
    AddBodyBullets $slide21 286 208 160 180 @('超过 single_purchase_limit_usdc 或 auto_purchase_threshold_usdc 必须确认','没有 confirm_over_threshold 参数就拒签','签名前后各做一次策略门校验') 12.5 | Out-Null
    AddTextBox $slide21 500 172 160 24 '本地状态加密' 18 "Microsoft YaHei UI" 0x5B412E 1 $true | Out-Null
    AddBodyBullets $slide21 500 208 160 180 @('master key 默认存在 ~/.stablepay-openclaw/master.key','state 文件用 AES-256-GCM 加密','debug 日志走 stderr，不污染工具 stdout') 12.5 | Out-Null
    AddTextBox $slide21 714 172 160 24 '质量保障' 18 "Microsoft YaHei UI" 0x2A557C 1 $true | Out-Null
    AddBodyBullets $slide21 714 208 160 180 @('eval trace 记录工具选择准确率、参数准确率、任务完成率','jargon leak 检测避免把底层术语暴露给用户','支付工具不是“能用就行”，还要评估体验与安全') 12.5 | Out-Null

    $slide22 = $pres.Slides.Item(22)
    ResetContentSlide $slide22 '3. 最终成果'
    AddCoverSummary $slide22 '成果' '我们不只做了一个 demo，而是跑通了“用户用得上、商家接得进、后端撑得住”的三条结果链路' '这一部分建议按用户侧、开发者侧、平台运行侧三页展开，让老板一眼看到成果不是停留在某个单点技术演示。'
    AddPanel $slide22 72 338 224 108 0xF7F2EA 0.0 1.0 | Out-Null
    AddPanel $slide22 338 338 224 108 0xF7F2EA 0.0 1.0 | Out-Null
    AddPanel $slide22 604 338 224 108 0xF7F2EA 0.0 1.0 | Out-Null
    AddTextBox $slide22 90 356 190 24 '用户闭环' 20 "Microsoft YaHei UI" 0x5B412E 1 $true | Out-Null
    AddTextBox $slide22 90 388 190 34 '开户、验证、充值、购买、到账都能走通。' 13 "Microsoft YaHei UI" 0x4C4C4C 1 $false | Out-Null
    AddTextBox $slide22 356 356 190 24 '开发者闭环' 20 "Microsoft YaHei UI" 0x5B412E 1 $true | Out-Null
    AddTextBox $slide22 356 388 190 34 '商家后端可返回 402，并依赖 verify 结果放行。' 13 "Microsoft YaHei UI" 0x4C4C4C 1 $false | Out-Null
    AddTextBox $slide22 622 356 190 24 '平台闭环' 20 "Microsoft YaHei UI" 0x5B412E 1 $true | Out-Null
    AddTextBox $slide22 622 388 190 34 '微服务、消息流与云端部署可稳定演示。' 13 "Microsoft YaHei UI" 0x4C4C4C 1 $false | Out-Null

    $slide23 = $pres.Slides.Item(23)
    ResetContentSlide $slide23 '3.1 用户侧闭环'
    AddTextBox $slide23 48 108 820 22 '从用户视角看，StablePay 最关键的价值是把“Agent 自己买东西”这件事，做成了可感知的一条顺滑流程。' 14.5 "Microsoft YaHei UI" 0x473728 1 $false | Out-Null
    AddStepCard $slide23 52 150 104 102 '1' '安装插件' '用户说一句“帮我初始化 StablePay 插件”。'
    AddStepCard $slide23 166 150 104 102 '2' '自动开户' '创建本地钱包并注册 DID。'
    AddStepCard $slide23 280 150 104 102 '3' '跳转验证' '打开 verify 链接完成 X 绑定。'
    AddStepCard $slide23 394 150 104 102 '4' '领取奖励' 'Verify & Claim 后收到 1 USDC。'
    AddStepCard $slide23 508 150 104 102 '5' '配置限额' '设置 50 USDC 总限额与 5 USDC 自动阈值。'
    AddStepCard $slide23 622 150 104 102 '6' '自动小额购买' '购买 3 USDC Skill，Agent 自动完成支付。'
    AddStepCard $slide23 736 150 104 102 '7' '大额人工确认' '购买 15 USDC Skill 时弹确认，再完成扣款。'
    AddPanel $slide23 92 286 332 148 0xF7F2EA 0.0 1.0 | Out-Null
    AddPanel $slide23 472 286 332 148 0xEEF5FB 0.0 1.0 0xC6DAEE | Out-Null
    AddTextBox $slide23 110 306 292 24 '自动支付场景' 18 "Microsoft YaHei UI" 0x5B412E 1 $true | Out-Null
    AddBodyBullets $slide23 110 340 286 72 @('3 USDC 商品低于自动阈值，客户端直接签名支付','支付后余额从 50 降到 47，形成清晰结果反馈') 12.8 | Out-Null
    AddTextBox $slide23 490 306 292 24 '确认支付场景' 18 "Microsoft YaHei UI" 0x2A557C 1 $true | Out-Null
    AddBodyBullets $slide23 490 340 286 72 @('15 USDC 商品超过阈值，不允许 LLM 自己做主','用户确认后再执行，余额从 47 降到 32') 12.8 | Out-Null

    $slide24 = $pres.Slides.Item(24)
    ResetContentSlide $slide24 '3.2 开发者侧闭环'
    AddTextBox $slide24 48 108 820 22 '我们同时验证了商家接入路径。样例商家后端不是“假装接入”，而是真的把 StablePay 当作一个外部支付底座来调用。' 14.5 "Microsoft YaHei UI" 0x473728 1 $false | Out-Null
    AddPanel $slide24 60 150 340 286 0xF7F2EA 0.0 1.0 | Out-Null
    AddTextBox $slide24 80 170 300 24 '接入步骤' 19 "Microsoft YaHei UI" 0x5B412E 1 $true | Out-Null
    AddBodyBullets $slide24 80 206 286 182 @('开发者创建钱包并注册 Skill DID','把 SKILL_DID 写进 skill.md 或 merchant 配置','merchant backend 的 /execute 先调 Verification /verify','未购买时返回 402，让客户端走支付重试链路','支付完成后重试业务请求，最终返回 200') 13 | Out-Null
    AddPanel $slide24 430 150 402 286 0xEEF5FB 0.0 1.0 0xC6DAEE | Out-Null
    AddTextBox $slide24 452 170 360 24 '为什么这条闭环重要' 19 "Microsoft YaHei UI" 0x2A557C 1 $true | Out-Null
    AddBodyBullets $slide24 452 206 332 182 @('商家不需要自己做链上支付，只需要会返回 402 challenge','StablePay 客户端拦截、签名、支付、重试全部自动完成','商家只关心“这个 Agent 有没有买过”，而不是钱包细节','这证明 StablePay 可以作为 Skill 市场的支付底层被复用') 13 | Out-Null

    $slide25 = $pres.Slides.Item(25)
    ResetContentSlide $slide25 '3.3 后端与云上运行'
    AddTextBox $slide25 48 108 820 22 '这一页先为你的实拍图留坑位。建议后续补上 kubectl、ACK 控制台、ACR 镜像列表和域名访问结果四类截图。' 14.5 "Microsoft YaHei UI" 0x473728 1 $false | Out-Null
    AddPanel $slide25 56 150 248 156 0xF8F5EE 0.0 1.0 | Out-Null
    AddPanel $slide25 322 150 248 156 0xF8F5EE 0.0 1.0 | Out-Null
    AddPanel $slide25 588 150 248 156 0xF8F5EE 0.0 1.0 | Out-Null
    AddTextBox $slide25 96 218 168 22 'ACR 镜像列表截图位' 17 "Microsoft YaHei UI" 0x8A715D 2 $true | Out-Null
    AddTextBox $slide25 362 218 168 22 'ACK Pod / Service 截图位' 17 "Microsoft YaHei UI" 0x8A715D 2 $true | Out-Null
    AddTextBox $slide25 628 218 168 22 'Ingress / 域名访问截图位' 17 "Microsoft YaHei UI" 0x8A715D 2 $true | Out-Null
    AddPanel $slide25 78 334 736 122 0xF7F2EA 0.0 1.0 | Out-Null
    AddBodyBullets $slide25 102 356 680 78 @('可以现场敲：kubectl get pods -n zheda-agent、kubectl get ingress -n zheda-agent、kubectl rollout status deployment/<svc>','讲法重点不是命令本身，而是“服务在跑、入口可达、版本能滚动更新、异常能回滚”','如果时间紧，这页甚至只放 2 张全景截图 + 3 条结果结论就够了') 13 | Out-Null

    $slide26 = $pres.Slides.Item(26)
    ResetContentSlide $slide26 '4. 总结与展望'
    AddCoverSummary $slide26 '复盘' '项目已经证明：Agent 自主支付这件事，不只是概念，已经能被拆成可工程化落地的一整套系统' '最后部分建议讲三件事：我们验证了什么、用户卡在哪里、接下来最值得继续做什么。'
    AddPanel $slide26 72 338 236 108 0xF7F2EA 0.0 1.0 | Out-Null
    AddPanel $slide26 338 338 236 108 0xF7F2EA 0.0 1.0 | Out-Null
    AddPanel $slide26 604 338 236 108 0xF7F2EA 0.0 1.0 | Out-Null
    AddTextBox $slide26 90 356 198 22 '验证了方向' 18 "Microsoft YaHei UI" 0x5B412E 1 $true | Out-Null
    AddTextBox $slide26 90 388 198 34 '402 + DID + 稳定币结算，确实能支撑 Agent 购买 Skill。' 13 "Microsoft YaHei UI" 0x4C4C4C 1 $false | Out-Null
    AddTextBox $slide26 356 356 198 22 '暴露了瓶颈' 18 "Microsoft YaHei UI" 0x5B412E 1 $true | Out-Null
    AddTextBox $slide26 356 388 198 34 '新手 onboarding、资金获取和客户端生态覆盖仍是门槛。' 13 "Microsoft YaHei UI" 0x4C4C4C 1 $false | Out-Null
    AddTextBox $slide26 622 356 198 22 '指向了下一步' 18 "Microsoft YaHei UI" 0x5B412E 1 $true | Out-Null
    AddTextBox $slide26 622 388 198 34 'MCP SDK、capability 复用、更多商家场景会把它推向平台化。' 13 "Microsoft YaHei UI" 0x4C4C4C 1 $false | Out-Null

    $slide27 = $pres.Slides.Item(27)
    ResetContentSlide $slide27 '4.1 用户测试反馈与改进'
    AddTextBox $slide27 48 108 820 22 '我们一共做了 10 人测试，最有价值的不是“顺不顺”，而是看清楚不同用户在什么地方卡住。' 14.5 "Microsoft YaHei UI" 0x473728 1 $false | Out-Null
    AddPanel $slide27 54 150 256 278 0xF7F2EA 0.0 1.0 | Out-Null
    AddPanel $slide27 324 150 256 278 0xF7F2EA 0.0 1.0 | Out-Null
    AddPanel $slide27 594 150 256 278 0xEEF5FB 0.0 1.0 0xC6DAEE | Out-Null
    AddTextBox $slide27 72 170 220 22 '用户画像' 18 "Microsoft YaHei UI" 0x5B412E 1 $true | Out-Null
    AddBodyBullets $slide27 72 204 212 172 @('纯小白：文档与 onboarding 是生死线','理工科 / Coding 用户：配置能过，但 USDC 获取困难','已有 OpenClaw 用户：整体最顺') 12.8 | Out-Null
    AddTextBox $slide27 342 170 220 22 '主要痛点' 18 "Microsoft YaHei UI" 0x5B412E 1 $true | Out-Null
    AddBodyBullets $slide27 342 204 212 172 @('onboarding 中会跳过 X 验证','奖励到账感知弱，闭环不强','1 USDC 启动资金门槛仍偏高','文档对非技术用户不够友好') 12.8 | Out-Null
    AddTextBox $slide27 612 170 220 22 '对应改进' 18 "Microsoft YaHei UI" 0x2A557C 1 $true | Out-Null
    AddBodyBullets $slide27 612 204 212 172 @('新增 x_verification 与 reward_claim 步骤','余额轮询确认奖励到账','手动水龙头 + 礼品码页面增强激励','用 Mintlify + llms.txt + MCP 重写文档入口') 12.8 | Out-Null

    $slide28 = $pres.Slides.Item(28)
    ClearBody $slide28 72
    AddTextBox $slide28 44 58 780 30 '4.2 技术栈收获与致谢' 26 "Microsoft YaHei UI" 0x1D3193 1 $true | Out-Null
    AddTextBox $slide28 48 108 820 22 '最后一页建议一半讲成长，一半讲感谢。这样既能收束技术价值，也能把合作支持清楚地交代出来。' 14.5 "Microsoft YaHei UI" 0x473728 1 $false | Out-Null
    AddPanel $slide28 56 150 382 286 0xF7F2EA 0.0 1.0 | Out-Null
    AddPanel $slide28 458 150 396 286 0xEEF5FB 0.0 1.0 0xC6DAEE | Out-Null
    AddTextBox $slide28 76 170 200 22 '技术栈与新体验' 19 "Microsoft YaHei UI" 0x5B412E 1 $true | Out-Null
    AddBodyBullets $slide28 76 206 326 186 @('第一次把 Kitex / Hertz / Thrift / RocketMQ / ACK 串成完整工程系统','学到的不是单点 API，而是如何把支付、身份、链上、部署和客户端协作起来','真正体验了一次从 PRD 到 demo 再到上云运维的工程闭环') 13 | Out-Null
    AddTextBox $slide28 478 170 180 22 '致谢' 19 "Microsoft YaHei UI" 0x2A557C 1 $true | Out-Null
    AddBodyBullets $slide28 478 206 334 186 @('感谢 StablePay 提供课题方向、技术方案支持与云资源','感谢 Jun Tan 在架构、排障与资源协调上的深度指导','感谢 X-Lab 提供真实项目实践机会','感谢团队同学从 1 月到期末周持续投入，把 5 万行工程真正跑起来') 13 | Out-Null

    for ($i = $pres.Slides.Count; $i -ge 29; $i--) {
        $pres.Slides.Item($i).Delete()
    }

    $pres.Save()
}
finally {
    if ($pres -ne $null) {
        $pres.Close()
        [void][System.Runtime.InteropServices.Marshal]::FinalReleaseComObject($pres)
    }
    if ($ppt -ne $null) {
        $ppt.Quit()
        [void][System.Runtime.InteropServices.Marshal]::FinalReleaseComObject($ppt)
    }
}
