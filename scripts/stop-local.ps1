$runDir = Join-Path (Split-Path $PSScriptRoot) ".local-run"
$pidDir = Join-Path $runDir "pids"

Get-ChildItem $pidDir -Filter "*.json" -ErrorAction SilentlyContinue | ForEach-Object {
    $record = Get-Content $_.FullName -Raw | ConvertFrom-Json

    if (Get-Process -Id $record.Pid -ErrorAction SilentlyContinue) {
        Write-Host "Stopping $($record.Name), PID=$($record.Pid)"
        & taskkill.exe /PID $record.Pid /T /F | Out-Null
    }

    Remove-Item $_.FullName -Force
}