param(
  [ValidateSet('amd64','arm64')][string]$Architecture = 'amd64'
)
$ErrorActionPreference = 'Stop'
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$releaseID = [DateTime]::UtcNow.ToString('yyyyMMdd-HHmmss') + "-linux-$Architecture"
$outputRoot = Join-Path $repoRoot '.local-run/procurement-deploy'
$releaseRoot = Join-Path $outputRoot $releaseID
New-Item -ItemType Directory -Path (Join-Path $releaseRoot 'bin') -Force | Out-Null
$saved = @{}
foreach ($name in @('GOOS','GOARCH','CGO_ENABLED','GOCACHE','GOMODCACHE','GOMAXPROCS')) { $saved[$name] = [Environment]::GetEnvironmentVariable($name, 'Process') }
try {
  $env:GOOS = 'linux'
  $env:GOARCH = $Architecture
  $env:CGO_ENABLED = '0'
  $env:GOMAXPROCS = '2'
  $env:GOCACHE = Join-Path (Split-Path $repoRoot -Parent) '.go-build-cache'
  $env:GOMODCACHE = Join-Path (Split-Path $repoRoot -Parent) '.go-mod-cache'
  foreach ($build in @(
    @{module='payment-service'; command='./cmd/fiat-server'; binary='payment-fiat'},
    @{module='commerce-runtime'; command='./cmd/procurement-mvp'; binary='procurement-runtime'}
  )) {
    Push-Location (Join-Path $repoRoot $build.module)
    try {
      & go build -p 2 -buildvcs=false -trimpath -ldflags '-s -w' -o (Join-Path $releaseRoot "bin/$($build.binary)") $build.command
      if ($LASTEXITCODE -ne 0) { throw "Linux build failed: $($build.module)" }
    } finally { Pop-Location }
  }
} finally {
  foreach ($name in $saved.Keys) { [Environment]::SetEnvironmentVariable($name, $saved[$name], 'Process') }
}
Push-Location (Join-Path $repoRoot 'stablepay-frontend')
try {
  if (-not (Test-Path -LiteralPath 'node_modules')) { & npm ci; if ($LASTEXITCODE -ne 0) { throw 'npm ci failed' } }
  & npm run build:payathon -- --outDir (Join-Path $releaseRoot 'web')
  if ($LASTEXITCODE -ne 0) { throw 'Frontend build failed' }
} finally { Pop-Location }
Copy-Item -LiteralPath (Join-Path $repoRoot 'deploy/procurement-mvp') -Destination (Join-Path $releaseRoot 'deploy') -Recurse
foreach ($textFile in Get-ChildItem -LiteralPath (Join-Path $releaseRoot 'deploy') -File) {
  [IO.File]::WriteAllText($textFile.FullName,[IO.File]::ReadAllText($textFile.FullName).Replace("`r`n","`n"),[Text.UTF8Encoding]::new($false))
}
$archive = Join-Path $outputRoot "$releaseID.tar.gz"
& tar.exe -czf $archive -C $outputRoot $releaseID
if ($LASTEXITCODE -ne 0) { throw 'Packaging failed' }
$sha256 = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant()
[IO.File]::WriteAllText("$archive.sha256", "$sha256  $releaseID.tar.gz`n", [Text.UTF8Encoding]::new($false))
Write-Output "Release: $archive"
Write-Output "SHA256: $sha256"
Write-Output 'Credentials are separate under .local-run/procurement-deploy/private; no secrets are included in the release.'
