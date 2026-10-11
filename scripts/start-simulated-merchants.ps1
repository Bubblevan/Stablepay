param(
    [switch]$Stop,
    [switch]$NoBuild
)

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
$datasetDir = Join-Path $repoRoot '.local-run\simulated-merchants'
$manifestPath = Join-Path $datasetDir 'manifest.json'
$statePath = Join-Path $datasetDir 'processes.json'
$binaryPath = Join-Path $datasetDir 'merchant-server.exe'
$runLogId = [DateTime]::UtcNow.ToString('yyyyMMddTHHmmssfffZ')

function Test-TcpPortListening([int]$Port) {
    try {
        return [bool](Test-NetConnection -ComputerName '127.0.0.1' -Port $Port `
            -InformationLevel Quiet -WarningAction SilentlyContinue)
    } catch {
        return $false
    }
}

if (-not (Test-Path -LiteralPath $manifestPath -PathType Leaf)) {
    throw "Simulated dataset not found: $manifestPath. Generate it with go run ./cmd/simulated-catalog from the merchant module."
}

$manifest = Get-Content -LiteralPath $manifestPath -Raw -Encoding UTF8 | ConvertFrom-Json
$expectedBinary = [IO.Path]::GetFullPath($binaryPath)

if ($Stop) {
    if (-not (Test-Path -LiteralPath $statePath -PathType Leaf)) {
        Write-Output 'No recorded simulated Merchant processes to stop.'
        exit 0
    }

    $state = Get-Content -LiteralPath $statePath -Raw | ConvertFrom-Json
    if ([IO.Path]::GetFullPath([string]$state.binary) -ne $expectedBinary) {
        throw 'Process state binary does not match this simulated dataset; refusing to stop processes.'
    }

    foreach ($entry in $state.processes) {
        $process = Get-Process -Id ([int]$entry.pid) -ErrorAction SilentlyContinue
        if ($null -eq $process) {
            Write-Output "Already stopped: $($entry.merchant_id) (pid $($entry.pid))"
            continue
        }

        $actualPath = ''
        try { $actualPath = [IO.Path]::GetFullPath($process.Path) } catch { }
        $actualStartTicks = 0L
        $recordedStartTicks = 0L
        try { $actualStartTicks = $process.StartTime.ToUniversalTime().Ticks } catch { }
        try { $recordedStartTicks = ([DateTime]$entry.started_utc).ToUniversalTime().Ticks } catch { }
        if ($actualPath -ne $expectedBinary -or $actualStartTicks -ne $recordedStartTicks) {
            throw "PID $($entry.pid) no longer identifies the recorded simulated Merchant; refusing to stop it."
        }
        Stop-Process -Id $process.Id
        Write-Output "Stopped: $($entry.merchant_id) (pid $($entry.pid))"
    }

    Remove-Item -LiteralPath $statePath
    exit 0
}

if (Test-Path -LiteralPath $statePath -PathType Leaf) {
    throw "A process state file already exists: $statePath. Inspect it and use -Stop before starting another set."
}

foreach ($merchant in $manifest.merchants) {
    if (Test-TcpPortListening ([int]$merchant.port)) {
        throw "Port $($merchant.port) for $($merchant.merchant_id) is already listening; no services were started."
    }
}

$merchantModule = Join-Path $repoRoot 'merchant'
if ($NoBuild) {
    if (-not (Test-Path -LiteralPath $binaryPath -PathType Leaf)) {
        throw "-NoBuild requires an existing Merchant server binary: $binaryPath"
    }
} else {
    Push-Location $merchantModule
    try {
        & go build -buildvcs=false -o $binaryPath ./cmd/merchant-server
        if ($LASTEXITCODE -ne 0) { throw "go build failed with exit code $LASTEXITCODE" }
    } finally {
        Pop-Location
    }
}

$started = [Collections.Generic.List[object]]::new()
try {
    foreach ($merchant in $manifest.merchants) {
        $merchantDir = Join-Path $datasetDir ([string]$merchant.merchant_id)
        $stdout = Join-Path $merchantDir "server-$runLogId.stdout.log"
        $stderr = Join-Path $merchantDir "server-$runLogId.stderr.log"
        $process = Start-Process -FilePath $binaryPath -WorkingDirectory $merchantDir `
            -WindowStyle Hidden -RedirectStandardOutput $stdout -RedirectStandardError $stderr -PassThru
        $started.Add([pscustomobject]@{
            merchant_id = [string]$merchant.merchant_id
            port = [int]$merchant.port
            pid = [int]$process.Id
            started_utc = $process.StartTime.ToUniversalTime().ToString('o')
        })
    }

    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    $pending = [Collections.Generic.List[object]]::new()
    foreach ($entry in $started) { $pending.Add($entry) }
    while ($pending.Count -gt 0 -and [DateTime]::UtcNow -lt $deadline) {
        for ($i = $pending.Count - 1; $i -ge 0; $i--) {
            $entry = $pending[$i]
            $process = Get-Process -Id $entry.pid -ErrorAction SilentlyContinue
            if ($null -eq $process) {
                $log = Join-Path (Join-Path $datasetDir $entry.merchant_id) 'server.stderr.log'
                throw "$($entry.merchant_id) exited before readiness. See $log"
            }
            try {
                $null = Invoke-RestMethod -Uri "http://127.0.0.1:$($entry.port)/healthz" -TimeoutSec 2
                $pending.RemoveAt($i)
                Write-Output "Ready: $($entry.merchant_id) http://127.0.0.1:$($entry.port) (pid $($entry.pid))"
            } catch { }
        }
        if ($pending.Count -gt 0) { Start-Sleep -Milliseconds 250 }
    }
    if ($pending.Count -gt 0) {
        throw "Timed out waiting for readiness: $((@($pending | ForEach-Object merchant_id)) -join ', ')"
    }

    $state = [pscustomobject]@{
        schema_version = 1
        binary = $expectedBinary
        processes = @($started)
    }
    $state | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $statePath -Encoding utf8
    Write-Output "Started $($started.Count) simulated Merchants. Stop them with: powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\start-simulated-merchants.ps1 -Stop"
} catch {
    foreach ($entry in $started) {
        $process = Get-Process -Id $entry.pid -ErrorAction SilentlyContinue
        if ($null -ne $process) {
            try {
                $actualPath = [IO.Path]::GetFullPath($process.Path)
                $actualStartTicks = $process.StartTime.ToUniversalTime().Ticks
                $recordedStartTicks = ([DateTime]$entry.started_utc).ToUniversalTime().Ticks
                if ($actualPath -eq $expectedBinary -and $actualStartTicks -eq $recordedStartTicks) {
                    Stop-Process -Id $process.Id -ErrorAction SilentlyContinue
                }
            } catch { }
        }
    }
    throw
}
