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
    [bool]$bold = $false,
    [int]$align = 1
) {
    $shape = $slide.Shapes.AddTextbox(1, (Pt $left), (Pt $top), (Pt $width), (Pt $height))
    $shape.Fill.Visible = 0
    $shape.Line.Visible = 0
    $shape.TextFrame.AutoSize = 0
    $shape.TextFrame.WordWrap = -1
    $shape.TextFrame.MarginLeft = 0
    $shape.TextFrame.MarginRight = 0
    $shape.TextFrame.MarginTop = 0
    $shape.TextFrame.MarginBottom = 0
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
    [double]$lineWeight = 0.8,
    [int]$lineRgb = 0xD5DCE5
) {
    $shape = $slide.Shapes.AddShape(5, (Pt $left), (Pt $top), (Pt $width), (Pt $height))
    $shape.Fill.ForeColor.RGB = $fillRgb
    $shape.Fill.Transparency = $transparency
    $shape.Line.ForeColor.RGB = $lineRgb
    $shape.Line.Weight = (Pt $lineWeight)
    return $shape
}

function AddBulletLines($slide, [double]$left, [double]$top, [double]$width, [double]$height, [string[]]$lines, [double]$fontSize = 15) {
    $text = ($lines | ForEach-Object { "• $_" }) -join "`r`n"
    $shape = AddTextBox $slide $left $top $width $height $text $fontSize "Microsoft YaHei UI" 0x4A4A4A $false 1
    $shape.TextFrame.TextRange.ParagraphFormat.SpaceAfter = 4
    return $shape
}

function RemoveBodyShapes($slide, [bool]$keepPictures = $false) {
    for ($i = $slide.Shapes.Count; $i -ge 1; $i--) {
        $shape = $slide.Shapes.Item($i)
        try {
            if ($shape.Top -lt (Pt 100)) { continue }
            if ($keepPictures -and $shape.Type -eq 13) { continue }
            $shape.Delete()
        } catch {}
    }
}

function SetTitleText($slide, [string]$title) {
    foreach ($shape in @($slide.Shapes)) {
        try {
            if ($shape.HasTextFrame -eq -1 -and $shape.TextFrame.HasText -eq -1) {
                if ($shape.Top -ge (Pt 68) -and $shape.Top -le (Pt 82) -and $shape.Width -ge (Pt 160)) {
                    $shape.TextFrame.TextRange.Text = $title
                    return
                }
            }
        } catch {}
    }
}

function BuildSlide16($slide) {
    RemoveBodyShapes $slide
    SetTitleText $slide '2.3 Agentic Plugin'
    AddTextBox $slide 37 114 820 36 '➢ 客户端层的意义，是把钱包、本地授权、状态恢复和宿主适配都收进同一层。' 17 "Microsoft YaHei UI" 0x3A3A3A $true | Out-Null
    AddPanel $slide 58 170 188 84 0xEEF5FB 0.0 0.8 0xAFC8E6 | Out-Null
    AddPanel $slide 260 170 188 84 0xEEF5FB 0.0 0.8 0xAFC8E6 | Out-Null
    AddPanel $slide 462 170 188 84 0xEEF5FB 0.0 0.8 0xAFC8E6 | Out-Null
    AddTextBox $slide 69 182 160 19 'OpenClaw' 18.5 "Microsoft YaHei UI" 0x30496D $true | Out-Null
    AddTextBox $slide 69 207 160 34 '已支撑飞书 / TUI，证明支付工具链能跑通。' 13.5 "Microsoft YaHei UI" 0x4A4A4A $false | Out-Null
    AddTextBox $slide 272 182 160 19 'MCP SDK' 18.5 "Microsoft YaHei UI" 0x30496D $true | Out-Null
    AddTextBox $slide 272 207 160 34 '让 Claude Code、Codex、Cursor 共享同一后端。' 13.5 "Microsoft YaHei UI" 0x4A4A4A $false | Out-Null
    AddTextBox $slide 474 182 160 19 'NPX CLI' 18.5 "Microsoft YaHei UI" 0x30496D $true | Out-Null
    AddTextBox $slide 474 207 160 34 '没有 IDE 宿主时，也能单独调试支付链路。' 13.5 "Microsoft YaHei UI" 0x4A4A4A $false | Out-Null
    AddPanel $slide 63 280 579 126 0xF8F5EF 0.0 0.8 0xE1D1B8 | Out-Null
    AddTextBox $slide 80 294 240 18 '客户端层补上的三件事' 16.5 "Microsoft YaHei UI" 0x6A4B2D $true | Out-Null
    AddBulletLines $slide 82 323 536 66 @(
        "把钱包、DID、签名和本地密钥都留在用户设备上，让私钥不出端。",
        "把 onboarding、doctor、限额和人工确认做成 Agent 可恢复、可继续的状态机。",
        "把后端微服务能力抽象成统一工具面，避免每个宿主各写一套支付接入逻辑。"
    ) 13.2 | Out-Null
    AddTextBox $slide 67 420 570 18 '一句话：后端解决“支付怎么完成”，插件层解决“Agent 怎么安全地把它用起来”。' 13.5 "Microsoft YaHei UI" 0x6B6B6B $false | Out-Null
}

function BuildSlide24($slide) {
    RemoveBodyShapes $slide $true
    SetTitleText $slide '3.2 开发者侧闭环'
    AddTextBox $slide 28.5 116 570 42 '➢ 样例 merchant backend 会主动返回 402 challenge，把 StablePay 当作外部支付底座来调用。' 17 "Microsoft YaHei UI" 0x3A3A3A $true | Out-Null
    AddPanel $slide 22 245 242 214 0xEEF5FB 0.0 0.8 0xAFC8E6 | Out-Null
    AddTextBox $slide 36 260 214 18 '接入步骤' 17.5 "Microsoft YaHei UI" 0x30496D $true | Out-Null
    AddBulletLines $slide 36 286 202 142 @(
        '开发者创建钱包并注册 Skill DID',
        '把 SKILL_DID 写进 skill.md 或 merchant 配置',
        'merchant backend 的 /execute 先调 Verification /verify',
        '未购买时返回 402，让客户端走支付重试链路',
        '支付完成后重试业务请求，最终返回 200'
    ) 12.5 | Out-Null
    AddPanel $slide 282 245 292 214 0xFAF4EC 0.0 0.8 0xE3C9A9 | Out-Null
    AddTextBox $slide 298 260 255 18 '为什么这条闭环重要' 17.5 "Microsoft YaHei UI" 0x6A4B2D $true | Out-Null
    AddBulletLines $slide 298 286 240 142 @(
        "商家不需要自己做链上支付，只需要会返回 402 challenge",
        "StablePay 客户端拦截、签名、支付、重试全部自动完成",
        "商家只关心这个 Agent 有没有买过，而不是钱包细节",
        "这证明 StablePay 可以作为 Skill 市场的支付底层被复用"
    ) 12.5 | Out-Null
    AddTextBox $slide 616 112 220 18 'Mintlify 文档站' 15 "Microsoft YaHei UI" 0x2F5D85 $true | Out-Null
    AddTextBox $slide 616 134 260 24 'Quickstart、API 参考和错误码集中在线。' 10.8 "Microsoft YaHei UI" 0x4A4A4A $false | Out-Null
    AddPanel $slide 620 452 292 48 0xF8F5EF 0.08 0.6 0xDCC9AE | Out-Null
    AddBulletLines $slide 632 463 266 24 @(
        '支持 docs.json 导航、双语页面，以及 Claude / Cursor / MCP 的 AI 原生入口'
    ) 11.4 | Out-Null
}

function BuildSlide25($slide) {
    RemoveBodyShapes $slide $true
    SetTitleText $slide '3.3 后端与云上运行'
    AddTextBox $slide 30 116 820 32 '➢ 这两张图要证明的是：集群长期稳定运行，而且公网入口与运维链路已经打通。' 17 "Microsoft YaHei UI" 0x3A3A3A $true | Out-Null
    AddPanel $slide 57 171 200 24 0x8B120B 0.0 0.0 | Out-Null
    AddTextBox $slide 67 174 180 16 '集群稳定运行' 14 "Microsoft YaHei UI" 0xFFFFFF $true | Out-Null
    AddTextBox $slide 270 171 600 22 '6 个业务服务和核心中间件统一跑在 ACK 上，关键 Pod 长期保持 Running。' 11.8 "Microsoft YaHei UI" 0x4A4A4A $false | Out-Null
    AddPanel $slide 57 437 200 24 0x8B120B 0.0 0.0 | Out-Null
    AddTextBox $slide 67 440 180 16 '入口与域名打通' 14 "Microsoft YaHei UI" 0xFFFFFF $true | Out-Null
    AddTextBox $slide 270 437 600 16 'ALB Ingress 将 ai.wenfu.cn 暴露到公网，后续发布、扩容和回滚都有统一入口承接流量。' 10.8 "Microsoft YaHei UI" 0x4A4A4A $false | Out-Null
}

function BuildSlide26($slide) {
    RemoveBodyShapes $slide
    SetTitleText $slide '4.1 技术栈与新体验'
    AddTextBox $slide 28.5 116 860 30 '➢ 这 100 多天最大的收获，不是会用了几个框架，而是第一次把一整条真实工程链完整跑通。' 16.5 "Microsoft YaHei UI" 0x3A3A3A $true | Out-Null
    AddPanel $slide 52 170 248 182 0xEEF5FB 0.0 0.8 0xAFC8E6 | Out-Null
    AddPanel $slide 327 170 248 182 0xEEF5FB 0.0 0.8 0xAFC8E6 | Out-Null
    AddPanel $slide 602 170 248 182 0xFAF4EC 0.0 0.8 0xE3C9A9 | Out-Null
    AddTextBox $slide 68 186 200 18 '工程栈' 16.5 "Microsoft YaHei UI" 0x30496D $true | Out-Null
    AddBulletLines $slide 68 214 200 112 @(
        "CloudWeGo：Hertz、Kitex、Thrift 让接口规范先行",
        "RocketMQ、MySQL、Redis、MongoDB 把支付链路撑起来",
        "Solana / USDC、ACK / ACR / ALB 让链上与云端真正联动"
    ) 12.2 | Out-Null
    AddTextBox $slide 343 186 200 18 '系统协作' 16.5 "Microsoft YaHei UI" 0x30496D $true | Out-Null
    AddBulletLines $slide 343 214 200 112 @(
        "不是单点写一个接口，而是让网关、支付、验签、验单、查询彼此配合",
        "不是本地跑通就算完成，而是要让插件、文档站和云端部署一起闭环",
        "不是写完代码就结束，还要会排障、回滚、做可观测与运维"
    ) 12.2 | Out-Null
    AddTextBox $slide 618 186 200 18 '工程体验' 16.5 "Microsoft YaHei UI" 0x6A4B2D $true | Out-Null
    AddBulletLines $slide 618 214 200 112 @(
        "从 PRD 出发，先写 IDL，再拆服务、接链、接宿主",
        "从 Minikube / 本地调试一路走到 ACK 集群上线",
        "从能跑进一步走到能演示、能解释、能迭代"
    ) 12.2 | Out-Null
    AddTextBox $slide 58 370 780 18 '一句话总结：这不是做完一个课程作业，而是真正做完了一套可落地、可交付、可复盘的工程系统。' 12.5 "Microsoft YaHei UI" 0x6B6B6B $false | Out-Null
}

function BuildSlide27($slide) {
    RemoveBodyShapes $slide
    SetTitleText $slide '4.2 致谢'
    AddTextBox $slide 28.5 116 820 28 '➢ 这套系统能从想法走到上线，不只是因为写了代码，更因为一路上有人一起把它推到了能落地的程度。' 16.5 "Microsoft YaHei UI" 0x3A3A3A $true | Out-Null
    AddPanel $slide 56 168 360 196 0xEEF5FB 0.0 0.8 0xAFC8E6 | Out-Null
    AddPanel $slide 452 168 386 196 0xFAF4EC 0.0 0.8 0xE3C9A9 | Out-Null
    AddTextBox $slide 76 184 220 18 '感谢企业与老师' 16.5 "Microsoft YaHei UI" 0x30496D $true | Out-Null
    AddBulletLines $slide 76 214 302 110 @(
        "感谢 StablePay 公司提供课题方向、PRD、技术方案支持与云资源",
        "感谢 Jun Tan 在架构设计、排障思路和资源协调上的深度指导",
        "感谢 X-Lab 提供把课堂知识搬进真实生产环境的机会"
    ) 12.5 | Out-Null
    AddTextBox $slide 472 184 220 18 '感谢团队' 16.5 "Microsoft YaHei UI" 0x6A4B2D $true | Out-Null
    AddBulletLines $slide 472 214 324 110 @(
        "感谢团队同学从 1 月开工到期末周仍然持续投入",
        "感谢大家从一行 Go import、一次 RocketMQ 故障、一次 Ingress 排查开始，把 5 万行工程真正跑起来",
        "这一百多天，我们真实地做了一回工程师"
    ) 12.5 | Out-Null
    AddTextBox $slide 92 390 700 20 '再次感谢 StablePay 给了我们一次 Change the world by programming 的真实实践机会。' 14 "Microsoft YaHei UI" 0x7A5D42 $true | Out-Null
}

if (-not (Test-Path -LiteralPath $InputPath)) {
    throw "Missing input PPTX: $InputPath"
}

$outDir = Split-Path -Parent $OutputPath
if (-not (Test-Path -LiteralPath $outDir)) {
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
}

Copy-Item -LiteralPath $InputPath -Destination $OutputPath -Force

$ppt = $null
$pres = $null

try {
    $ppt = New-Object -ComObject PowerPoint.Application
    $ppt.Visible = -1
    $pres = $ppt.Presentations.Open($OutputPath, $false, $false, $false)

    BuildSlide16 $pres.Slides.Item(16)
    BuildSlide24 $pres.Slides.Item(24)
    BuildSlide25 $pres.Slides.Item(25)
    BuildSlide26 $pres.Slides.Item(26)
    BuildSlide27 $pres.Slides.Item(27)

    for ($i = $pres.Slides.Count; $i -ge 28; $i--) {
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
