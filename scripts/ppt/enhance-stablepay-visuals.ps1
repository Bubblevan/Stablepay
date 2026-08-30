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

function Remove-ShapeIfExists($slide, [int]$id) {
    foreach ($shape in @($slide.Shapes)) {
        try {
            if ($shape.Id -eq $id) {
                $shape.Delete()
                break
            }
        }
        catch {}
    }
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
        $shape.Line.ForeColor.RGB = $fillRgb
        $shape.Line.Weight = (Pt $lineWeight)
    }
    return $shape
}

function AddChip($slide, [double]$left, [double]$top, [double]$width, [double]$height, [string]$title, [string]$body, [int]$fillRgb) {
    $panel = AddPanel $slide $left $top $width $height $fillRgb 0.06 1.1
    $panel.Line.ForeColor.RGB = 0xD7C8AA
    $panel.Fill.ForeColor.RGB = $fillRgb
    AddTextBox $slide ($left + 12) ($top + 8) ($width - 24) 24 $title 16 "Microsoft YaHei UI" 0x2C2C2C 1 $true | Out-Null
    AddTextBox $slide ($left + 12) ($top + 33) ($width - 24) ($height - 38) $body 11.5 "Microsoft YaHei UI" 0x4F4F4F 1 $false | Out-Null
}

if (-not (Test-Path -LiteralPath $InputPath)) {
    throw "Missing input PPTX: $InputPath"
}

$outDir = Split-Path -Parent $OutputPath
if (-not (Test-Path -LiteralPath $outDir)) {
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
}

Copy-Item -LiteralPath $InputPath -Destination $OutputPath -Force

$coreImage = Join-Path (Get-Location) "outputs\\ai-images\\stablepay-core-tech.png"
$servicesImage = Join-Path (Get-Location) "outputs\\ai-images\\stablepay-microservices.png"
$gatewayImage = Join-Path (Get-Location) "outputs\\ai-images\\stablepay-api-gateway.png"

foreach ($img in @($coreImage, $servicesImage, $gatewayImage)) {
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

    $slide7Intro = '我们把 Agent 支付拆成「本地身份与策略 + 云端编排 + 链上结算」三层，让它既能自主执行，又不会失控。'
    $slide7Title = '三层设计'
    $slide7Client = '客户端层' + "`n" + '本地钱包、DID、额度策略'
    $slide7Service = '服务层' + "`n" + '网关、支付、验证、查询协作'
    $slide7Chain = '结算层' + "`n" + 'Solana + USDC 完成最终转账'
    $slide7Footer = '设计目标不是炫技，而是让 Agent 的每一笔支付都「能做、敢做、查得清」。'

    $slide8Intro = '后端不是一个「大接口」，而是把身份、支付、查询、验单拆成独立服务，再用 API Gateway 和消息队列把它们组织起来。'
    $slide8Chip1Title = '同步调用'
    $slide8Chip1Body = '网关分发后，关键链路通过 Kitex RPC 快速完成。'
    $slide8Chip2Title = '异步解耦'
    $slide8Chip2Body = '支付成功后发布事件，验证服务异步落购买记录。'
    $slide8Chip3Title = '工程价值'
    $slide8Chip3Body = '单个服务职责更清晰，也更适合后续扩容与迭代。'

    $slide9Intro = 'API Gateway 是整套系统的「总门厅」：所有外部请求先到这里，再决定谁能进、怎么进、该发往哪个服务。'
    $slide9PanelTitle = '它主要做四件事'
    $slide9Item1 = '1. 统一入口' + "`n" + '把不同接口都收口到同一扇门前'
    $slide9Item2 = '2. 鉴权验签' + "`n" + '确认请求确实来自对应 DID'
    $slide9Item3 = '3. 防重放与限流' + "`n" + '拦住重复请求和异常流量'
    $slide9Item4 = '4. 路由分发' + "`n" + '把支付、查询、验证请求发到正确服务'
    $slide9Footer = '从代码实现上，它把鉴权、限流、可用性保护做成中间件链，因此后续加新接口也能复用同一套安全框架。'

    $slide7 = $pres.Slides.Item(7)
    foreach ($id in @(4, 1081)) { Remove-ShapeIfExists $slide7 $id }
    $slide7.Shapes.AddPicture($coreImage, $false, $true, (Pt 84), (Pt 172), (Pt 592), (Pt 270)) | Out-Null
    AddTextBox $slide7 86 128 720 26 $slide7Intro 14.5 "Microsoft YaHei UI" 0x473728 1 $false | Out-Null
    $panel7 = AddPanel $slide7 690 176 180 254 0xF6F0E5 0.03 1.0
    $panel7.Line.ForeColor.RGB = 0xE3D5BD
    AddTextBox $slide7 706 178 148 26 $slide7Title 18 "Microsoft YaHei UI" 0x5A402D 1 $true | Out-Null
    AddTextBox $slide7 706 212 148 54 $slide7Client 14 "Microsoft YaHei UI" 0x4F4F4F 1 $false | Out-Null
    AddTextBox $slide7 706 282 148 54 $slide7Service 14 "Microsoft YaHei UI" 0x4F4F4F 1 $false | Out-Null
    AddTextBox $slide7 706 352 148 54 $slide7Chain 14 "Microsoft YaHei UI" 0x4F4F4F 1 $false | Out-Null
    AddTextBox $slide7 84 452 612 22 $slide7Footer 13 "Microsoft YaHei UI" 0x7B6856 1 $false | Out-Null

    $slide8 = $pres.Slides.Item(8)
    foreach ($id in @(1081)) { Remove-ShapeIfExists $slide8 $id }
    $slide8.Shapes.AddPicture($servicesImage, $false, $true, (Pt 72), (Pt 150), (Pt 792), (Pt 280)) | Out-Null
    AddTextBox $slide8 84 126 770 24 $slide8Intro 14.5 "Microsoft YaHei UI" 0x473728 1 $false | Out-Null
    AddChip $slide8 82 420 210 72 $slide8Chip1Title $slide8Chip1Body 0xF7EEDC
    AddChip $slide8 314 420 246 72 $slide8Chip2Title $slide8Chip2Body 0xF7EEDC
    AddChip $slide8 582 420 246 72 $slide8Chip3Title $slide8Chip3Body 0xF7EEDC

    $slide9 = $pres.Slides.Item(9)
    foreach ($id in @(3, 4, 5, 6, 7, 8, 9, 10)) { Remove-ShapeIfExists $slide9 $id }
    $slide9.Shapes.AddPicture($gatewayImage, $false, $true, (Pt 74), (Pt 162), (Pt 472), (Pt 292)) | Out-Null
    AddTextBox $slide9 86 126 760 24 $slide9Intro 14.5 "Microsoft YaHei UI" 0x473728 1 $false | Out-Null
    $rightPanel = AddPanel $slide9 568 170 302 276 0xF6F0E5 0.02 1.1
    $rightPanel.Line.ForeColor.RGB = 0xE2D2B6
    AddTextBox $slide9 588 172 262 26 $slide9PanelTitle 18 "Microsoft YaHei UI" 0x5A402D 1 $true | Out-Null
    AddTextBox $slide9 590 212 250 34 $slide9Item1 14 "Microsoft YaHei UI" 0x4B4B4B 1 $false | Out-Null
    AddTextBox $slide9 590 266 250 34 $slide9Item2 14 "Microsoft YaHei UI" 0x4B4B4B 1 $false | Out-Null
    AddTextBox $slide9 590 320 250 34 $slide9Item3 14 "Microsoft YaHei UI" 0x4B4B4B 1 $false | Out-Null
    AddTextBox $slide9 590 374 250 34 $slide9Item4 14 "Microsoft YaHei UI" 0x4B4B4B 1 $false | Out-Null
    AddTextBox $slide9 74 458 790 18 $slide9Footer 12.5 "Microsoft YaHei UI" 0x7B6856 1 $false | Out-Null

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
