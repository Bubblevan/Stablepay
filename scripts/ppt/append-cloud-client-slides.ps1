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
    [double]$fontSize,
    [string]$fontName,
    [int]$rgb,
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
    [double]$lineWeight = 0.0
) {
    $shape = $slide.Shapes.AddShape(5, (Pt $left), (Pt $top), (Pt $width), (Pt $height))
    $shape.Fill.Visible = -1
    $shape.Fill.ForeColor.RGB = $fillRgb
    $shape.Fill.Transparency = $transparency
    if ($lineWeight -le 0) {
        $shape.Line.Visible = 0
    } else {
        $shape.Line.Visible = -1
        $shape.Line.ForeColor.RGB = 0xD9C8A8
        $shape.Line.Weight = (Pt $lineWeight)
    }
    return $shape
}

function AddChip($slide, [double]$left, [double]$top, [double]$width, [double]$height, [string]$title, [string]$body) {
    $panel = AddPanel $slide $left $top $width $height 0xF7EEDC 0.02 1.0
    AddTextBox $slide ($left + 12) ($top + 8) ($width - 24) 20 $title 17 "Microsoft YaHei UI" 0x2F2F2F 1 $true | Out-Null
    AddTextBox $slide ($left + 12) ($top + 30) ($width - 24) ($height - 34) $body 11.5 "Microsoft YaHei UI" 0x555555 1 $false | Out-Null
}

function ClearBody($slide) {
    $toDelete = @()
    foreach ($shape in @($slide.Shapes)) {
        try {
            if ($shape.Top -ge (Pt 100)) {
                $toDelete += $shape
            }
        }
        catch {}
    }
    foreach ($shape in $toDelete) {
        try { $shape.Delete() } catch {}
    }
}

function SetSlideTitle($slide, [string]$title) {
    foreach ($shape in @($slide.Shapes)) {
        try {
            if ($shape.HasTextFrame -eq -1 -and $shape.TextFrame.HasText -eq -1) {
                if ($shape.Top -ge (Pt 60) -and $shape.Top -le (Pt 90) -and $shape.Width -ge (Pt 220)) {
                    $range = $shape.TextFrame.TextRange
                    $range.Text = $title
                    $range.Font.NameFarEast = "Microsoft YaHei UI"
                    $range.Font.Name = "Microsoft YaHei UI"
                    $range.Font.Bold = -1
                    $range.Font.Color.RGB = 0x8B120B
                    return
                }
            }
        }
        catch {}
    }
}

if (-not (Test-Path -LiteralPath $InputPath)) {
    throw "Missing input PPTX: $InputPath"
}

$outDir = Split-Path -Parent $OutputPath
if (-not (Test-Path -LiteralPath $outDir)) {
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
}

Copy-Item -LiteralPath $InputPath -Destination $OutputPath -Force

$cloudArch = Join-Path (Get-Location) "outputs\\ai-images\\stablepay-cloud-arch-white.png"
$cicdOps = Join-Path (Get-Location) "outputs\\ai-images\\stablepay-cicd-ops-white.png"
$clientEco = Join-Path (Get-Location) "outputs\\ai-images\\stablepay-client-ecosystem-white.png"
$clientFlow = Join-Path (Get-Location) "outputs\\ai-images\\stablepay-client-flow-white.png"

foreach ($img in @($cloudArch, $cicdOps, $clientEco, $clientFlow)) {
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

    $templateSlide = $pres.Slides.Item(14)
    for ($i = 0; $i -lt 6; $i++) {
        [void]$templateSlide.Duplicate()
        $templateSlide = $pres.Slides.Item($pres.Slides.Count)
    }

    $slide15 = $pres.Slides.Item(15)
    ClearBody $slide15
    SetSlideTitle $slide15 '3.1 云原生整体部署架构'
    AddTextBox $slide15 48 108 820 24 '我们把多微服务系统真正部署到云端，形成从本地开发、镜像构建、集群运行到公网访问的完整交付链路。' 15 "Microsoft YaHei UI" 0x473728 1 $false | Out-Null
    $slide15.Shapes.AddPicture($cloudArch, $false, $true, (Pt 48), (Pt 138), (Pt 840), (Pt 260)) | Out-Null
    AddChip $slide15 70 414 170 58 'ACK 容器编排' '统一承载业务服务与中间件运行。'
    AddChip $slide15 260 414 170 58 'ACR 镜像仓库' '镜像统一托管，发布入口标准化。'
    AddChip $slide15 450 414 170 58 '消息与存储' 'RocketMQ、MySQL、Redis 共同支撑交易链路。'
    AddChip $slide15 640 414 170 58 '公网接入' 'ALB 统一承接外部访问与域名流量。'

    $slide16 = $pres.Slides.Item(16)
    ClearBody $slide16
    SetSlideTitle $slide16 '3.2 自动化 CI/CD 与运维攻坚'
    AddTextBox $slide16 48 108 820 24 '这一页重点不是“用了什么云服务”，而是我们把发布流程自动化，并补上了实际运维中的关键短板。' 15 "Microsoft YaHei UI" 0x473728 1 $false | Out-Null
    $slide16.Shapes.AddPicture($cicdOps, $false, $true, (Pt 48), (Pt 140), (Pt 840), (Pt 248)) | Out-Null
    AddChip $slide16 84 402 200 66 '无人值守发布' '把代码提交、构建、推送、部署串成标准化流水线。'
    AddChip $slide16 316 402 220 66 'Topic 自动初始化' '用 Job 根治 RocketMQ 重启后主题丢失的经典运维坑。'
    AddChip $slide16 566 402 220 66 '分钟级止损' '保留一键回滚能力，让异常版本快速撤回。'

    $slide17 = $pres.Slides.Item(17)
    ClearBody $slide17
    SetSlideTitle $slide17 '3.3 部署成果与稳定运行'
    AddTextBox $slide17 48 108 820 24 '这一页先预留给你放真实截图，建议展示 ACK 集群全景、Pod 状态、Ingress/域名接入结果。' 15 "Microsoft YaHei UI" 0x473728 1 $false | Out-Null
    $leftBox = AddPanel $slide17 68 154 360 250 0xF8F5EE 0.0 1.2
    $rightBox = AddPanel $slide17 472 154 360 250 0xF8F5EE 0.0 1.2
    AddTextBox $slide17 120 258 256 28 'ACK 集群 / Pod 运行截图占位区' 18 "Microsoft YaHei UI" 0x8A715D 1 $true | Out-Null
    AddTextBox $slide17 526 258 252 28 'Ingress / 域名访问 / 日志截图占位区' 18 "Microsoft YaHei UI" 0x8A715D 1 $true | Out-Null
    AddTextBox $slide17 92 420 716 20 '建议截图优先级：1. Pod 全绿运行  2. 服务与 Ingress 已暴露  3. 域名可访问或滚动更新成功。' 13 "Microsoft YaHei UI" 0x7B6856 1 $false | Out-Null

    $slide18 = $pres.Slides.Item(18)
    ClearBody $slide18
    SetSlideTitle $slide18 '4.1 客户端生态矩阵与升级路线'
    AddTextBox $slide18 48 108 820 24 '我们没有把支付客户端绑定在单一宿主上，而是规划成从 OpenClaw 走向 MCP 与 CLI 的双轨制升级路径。' 15 "Microsoft YaHei UI" 0x473728 1 $false | Out-Null
    $slide18.Shapes.AddPicture($clientEco, $false, $true, (Pt 54), (Pt 136), (Pt 828), (Pt 290)) | Out-Null
    AddTextBox $slide18 90 432 700 20 '产品定位从“服务一个宿主”升级为“服务整个 Agent 生态”，这决定了后续扩展空间。' 13 "Microsoft YaHei UI" 0x7B6856 1 $false | Out-Null

    $slide19 = $pres.Slides.Item(19)
    ClearBody $slide19
    SetSlideTitle $slide19 '4.2 核心能力与用户全流程'
    AddTextBox $slide19 48 108 820 24 '客户端不是若干零散工具，而是一条完整的使用路径：从本地身份初始化，到验证、配置、再到自主支付。' 15 "Microsoft YaHei UI" 0x473728 1 $false | Out-Null
    $slide19.Shapes.AddPicture($clientFlow, $false, $true, (Pt 46), (Pt 136), (Pt 844), (Pt 286)) | Out-Null
    AddChip $slide19 90 424 180 58 '身份能力' '本地钱包、DID 注册、签名与身份管理。'
    AddChip $slide19 312 424 180 58 '支付能力' '策略构建、余额查询、支付执行与账单结果。'
    AddChip $slide19 534 424 180 58 '验证与商家' 'X 验证、商家接入、销售与收益查询。'

    $slide20 = $pres.Slides.Item(20)
    ClearBody $slide20
    SetSlideTitle $slide20 '4.3 安全防护与质量保障'
    AddTextBox $slide20 48 108 820 24 'AI 自主支付最关键的问题不是“能不能付”，而是“能不能在可控边界内放心地付”。' 15 "Microsoft YaHei UI" 0x473728 1 $false | Out-Null
    $leftPanel = AddPanel $slide20 60 160 360 280 0xF7EEDC 0.0 1.1
    $rightPanel = AddPanel $slide20 470 160 360 280 0xEEF5FB 0.0 1.1
    AddTextBox $slide20 82 178 150 24 '安全防护' 20 "Microsoft YaHei UI" 0x5A402D 1 $true | Out-Null
    AddTextBox $slide20 82 214 300 138 ('1. 失败状态显式返回' + "`n" + '杜绝 LLM 把“有返回”误判成“支付成功”。' + "`n`n" + '2. 大额支付强制二次确认' + "`n" + '超过自动阈值必须人工确认，限制资金风险。' + "`n`n" + '3. 本地密钥 AES-256-GCM 加密' + "`n" + '把钱包与本地状态保护在用户设备上。') 14 "Microsoft YaHei UI" 0x4C4C4C 1 $false | Out-Null
    AddTextBox $slide20 492 178 170 24 '质量保障' 20 "Microsoft YaHei UI" 0x274A67 1 $true | Out-Null
    AddTextBox $slide20 492 214 300 138 ('1. doctor 先诊断再支付' + "`n" + '在执行前识别缺失条件，降低错误路径。' + "`n`n" + '2. onboard 状态机可恢复' + "`n" + '支持中断后继续，减少用户重复操作。' + "`n`n" + '3. eval + trace 留痕' + "`n" + '工具选择、参数填写、任务完成率都可以追踪评估。') 14 "Microsoft YaHei UI" 0x4C4C4C 1 $false | Out-Null
    AddTextBox $slide20 86 452 694 18 '我们的目标不是让 AI 拥有无限支付权限，而是让它在规则、审计和确认机制下安全完成任务。' 12.5 "Microsoft YaHei UI" 0x7B6856 1 $false | Out-Null

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
