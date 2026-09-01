$ErrorActionPreference = "Stop"

$root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$runDir = Join-Path $root ".local-run"
$logDir = Join-Path $runDir "logs"
$pidDir = Join-Path $runDir "pids"

New-Item -ItemType Directory -Force -Path $logDir, $pidDir | Out-Null

$hotWallet = Join-Path $root "blockchain-adapter\config\hotwallet.json"
if (-not (Test-Path -LiteralPath $hotWallet)) {
    Write-Warning "Missing $hotWallet; blockchain-adapter will exit until a real Devnet hot wallet is provided."
}

$services = @(
    @{
        Name = "did-service"
        Dir = "did-service"
        Command = "`$env:CONFIG_PATH='config/dev.yaml'; & go run ./cmd/server"
    },
    @{
        Name = "blockchain-adapter"
        Dir = "blockchain-adapter"
        Command = "& go run ./cmd/server -config ./config/dev.local.yaml"
    },
    @{
        Name = "query-service"
        Dir = "query-service"
        Command = "`$env:MYSQL_HOST='127.0.0.1'; `$env:MYSQL_PORT='3307'; & go run ./cmd/server"
    },
    @{
        Name = "verification-service"
        Dir = "verification-service"
        Command = "`$env:MYSQL_HOST='127.0.0.1'; `$env:MYSQL_PORT='3307'; `$env:ROCKETMQ_NAMESERVER='127.0.0.1:9876'; & go run ./cmd/server"
    },
    @{
        Name = "payment-service"
        Dir = "payment-service"
        Command = "`$env:CONFIG_PATH='config/config.local.yaml'; & go run ./cmd/server"
    },
    @{
        Name = "api-gateway"
        Dir = "api-gateway"
        Command = "& go run ./cmd/server -config ./config/config.yaml"
    }
)

foreach ($svc in $services) {
    $workDir = Join-Path $root $svc.Dir
    $stdout = Join-Path $logDir "$($svc.Name).out.log"
    $stderr = Join-Path $logDir "$($svc.Name).err.log"
    $pidFile = Join-Path $pidDir "$($svc.Name).json"

    $command = "Set-Location -LiteralPath '$workDir'; $($svc.Command)"

    $process = Start-Process `
        -FilePath "powershell.exe" `
        -ArgumentList @("-NoLogo", "-NoProfile", "-NonInteractive", "-Command", $command) `
        -RedirectStandardOutput $stdout `
        -RedirectStandardError $stderr `
        -PassThru

    @{
        Name = $svc.Name
        Pid = $process.Id
        StartedAt = (Get-Date).ToString("s")
    } | ConvertTo-Json | Set-Content -Encoding UTF8 $pidFile

    Write-Host "Started $($svc.Name), PID=$($process.Id)"

    # Give dependency services a moment to bind their RPC ports before the next service starts.
    Start-Sleep -Seconds 2
}

Write-Host ""
Write-Host "Logs: $logDir"
Write-Host "PIDs : $pidDir"
