[CmdletBinding()]
param(
    [int]$Count = 10,
    [ValidateRange(1, 10)]
    [int]$Concurrency = 3,
    [string]$BaseUrl = 'http://127.0.0.1:18091',
    [string]$Token = $env:MINIKUBE_E4_API_TOKEN,
    [string]$OutputRoot = ''
)

$ErrorActionPreference = 'Stop'
if ($Count -lt 1 -or $Count -gt 50) { throw 'Count must be between 1 and 50 for this small local run' }
if ([string]::IsNullOrWhiteSpace($Token)) { throw 'Set MINIKUBE_E4_API_TOKEN or pass -Token from the local Secret' }
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$bodyPath = Join-Path $repoRoot 'testdata\benchmark\e4-submit-body.json'
if ([string]::IsNullOrWhiteSpace($OutputRoot)) {
    $OutputRoot = Join-Path $repoRoot ('.local-run\minikube-evidence\load-' + [DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ'))
}
$OutputRoot = [System.IO.Path]::GetFullPath($OutputRoot)
New-Item -ItemType Directory -Force -Path $OutputRoot | Out-Null

$worker = {
    param($BaseUrl, $Token, $BodyPath, $RequestId)
    $ErrorActionPreference = 'Stop'
    $timer = [Diagnostics.Stopwatch]::StartNew()
    try {
        $body = Get-Content -Raw -Encoding UTF8 -LiteralPath $BodyPath | ConvertFrom-Json
        $body.request_id = $RequestId
        $body.parent_session_id = "minikube-e2e-$RequestId"
        $body.input.uri = "local://minikube/$RequestId"
        $headers = @{ Authorization = "Bearer $Token"; 'Idempotency-Key' = $RequestId }
        $submitted = Invoke-RestMethod -Method Post -Uri "$BaseUrl/v1/episodes" -Headers $headers -ContentType 'application/json' -Body ($body | ConvertTo-Json -Depth 20 -Compress)
        $episodeId = [string]$submitted.episode.episode_id
        if ([string]::IsNullOrWhiteSpace($episodeId)) { throw 'submit response did not contain episode_id' }

        $deadline = [DateTime]::UtcNow.AddSeconds(60)
        $status = $null
        do {
            $status = Invoke-RestMethod -Method Get -Uri "$BaseUrl/v1/episodes/$([uri]::EscapeDataString($episodeId))" -Headers $headers
            $state = [string]$status.episode.state
            if ($state -in @('FULFILLED', 'FAILED', 'BLOCKED', 'ABORTED', 'EXPIRED', 'DISPUTED')) { break }
            Start-Sleep -Milliseconds 200
        } while ([DateTime]::UtcNow -lt $deadline)

        $events = Invoke-RestMethod -Method Get -Uri "$BaseUrl/v1/episodes/$([uri]::EscapeDataString($episodeId))/events" -Headers $headers
        $timer.Stop()
        [pscustomobject]@{
            request_id = $RequestId
            episode_id = $episodeId
            state = [string]$status.episode.state
            execution_status = [string]$status.execution.status
            event_count = @($events.events).Count
            state_timeline = @($events.events | ForEach-Object { [string]$_.state_after })
            elapsed_ms = [math]::Round($timer.Elapsed.TotalMilliseconds, 1)
            error = $null
        }
    } catch {
        $timer.Stop()
        [pscustomobject]@{
            request_id = $RequestId
            episode_id = $null
            state = 'ERROR'
            execution_status = 'ERROR'
            event_count = 0
            state_timeline = @()
            elapsed_ms = [math]::Round($timer.Elapsed.TotalMilliseconds, 1)
            error = $_.Exception.Message
        }
    }
}

$timerAll = [Diagnostics.Stopwatch]::StartNew()
$results = [System.Collections.Generic.List[object]]::new()
for ($first = 1; $first -le $Count; $first += $Concurrency) {
    $jobs = @()
    $last = [Math]::Min($Count, $first + $Concurrency - 1)
    for ($index = $first; $index -le $last; $index++) {
        $requestId = 'minikube-load-' + [DateTime]::UtcNow.ToString('yyyyMMddHHmmssfff') + '-' + $index + '-' + [guid]::NewGuid().ToString('N').Substring(0, 8)
        $jobs += Start-Job -ScriptBlock $worker -ArgumentList $BaseUrl, $Token, $bodyPath, $requestId
    }
    foreach ($job in $jobs) {
        $value = Receive-Job -Job $job -Wait
        if ($value) { $results.Add($value) }
        Remove-Job -Job $job -Force
    }
}
$timerAll.Stop()

$orderedMs = @($results | ForEach-Object { [double]$_.elapsed_ms } | Sort-Object)
$p50 = if ($orderedMs.Count) { $orderedMs[[Math]::Ceiling(0.50 * $orderedMs.Count) - 1] } else { $null }
$p95 = if ($orderedMs.Count) { $orderedMs[[Math]::Ceiling(0.95 * $orderedMs.Count) - 1] } else { $null }
$successes = @($results | Where-Object { $_.state -eq 'FULFILLED' -and $_.execution_status -eq 'COMPLETED' }).Count
$summary = [ordered]@{
    benchmark = 'Minikube E4 local state-machine load'
    status = if ($successes -eq $Count -and $results.Count -eq $Count) { 'PASS' } else { 'FAIL' }
    adapter_mode = 'benchmark-only deterministic local Merchant/Payment/Verification mocks; no chain client'
    profile = 'stablepay-multi'
    physical_hosts = 1
    kubernetes_nodes = 3
    request_count = $Count
    concurrency = $Concurrency
    completed = $successes
    failed_or_nonterminal = $Count - $successes
    elapsed_seconds = [math]::Round($timerAll.Elapsed.TotalSeconds, 3)
    completed_episodes_per_second = if ($timerAll.Elapsed.TotalSeconds -gt 0) { [math]::Round($successes / $timerAll.Elapsed.TotalSeconds, 3) } else { 0 }
    episode_latency_ms = @{ p50 = $p50; p95 = $p95; method = 'nearest-rank' }
    output = 'load-results.json'
}
$summary | ConvertTo-Json -Depth 10 | Set-Content -Encoding UTF8 -LiteralPath (Join-Path $OutputRoot 'load-summary.json')
@($results) | ConvertTo-Json -Depth 10 | Set-Content -Encoding UTF8 -LiteralPath (Join-Path $OutputRoot 'load-results.json')
$summary | ConvertTo-Json -Depth 10
if ($summary.status -ne 'PASS') { exit 1 }
