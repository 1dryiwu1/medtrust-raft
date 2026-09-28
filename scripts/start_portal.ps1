param(
    [ValidateSet('start', 'stop')]
    [string]$Action = 'start'
)

$ErrorActionPreference = 'Stop'
$ProjectRoot = Split-Path -Parent $PSScriptRoot
$PidFile = Join-Path $ProjectRoot 'logs\portal-cluster.pids'
$Binary = Join-Path $ProjectRoot 'medtrust-node.exe'

if ($Action -eq 'stop') {
    if (-not (Test-Path -LiteralPath $PidFile)) {
        Write-Host 'No portal cluster PID file was found.'
        exit 0
    }
    Get-Content -LiteralPath $PidFile | ForEach-Object {
        $ProcessId = 0
        if ([int]::TryParse($_, [ref]$ProcessId)) {
			$Process = Get-Process -Id $ProcessId -ErrorAction SilentlyContinue
			if ($null -ne $Process -and $Process.Path -eq $Binary) {
				Stop-Process -Id $ProcessId
			}
        }
    }
    Remove-Item -LiteralPath $PidFile -Force
    Write-Host 'MedTrust portal cluster stopped.'
    exit 0
}

Set-Location $ProjectRoot
New-Item -ItemType Directory -Force -Path (Join-Path $ProjectRoot 'logs') | Out-Null

Push-Location (Join-Path $ProjectRoot 'web')
try {
    cmd /c npm run build
    if ($LASTEXITCODE -ne 0) { throw 'Frontend build failed.' }
} finally {
    Pop-Location
}

go build -o $Binary ./cmd/node
if ($LASTEXITCODE -ne 0) { throw 'Go build failed.' }

$Started = 1..3 | ForEach-Object {
    $Config = Join-Path $ProjectRoot "configs\node$_.yaml"
    $Process = Start-Process -FilePath $Binary -ArgumentList @('--config', $Config) -PassThru -WindowStyle Hidden
    $Process.Id
}
$Started | Set-Content -LiteralPath $PidFile
Start-Sleep -Seconds 2
Write-Host 'MedTrust portal is running at http://127.0.0.1:8001'
Write-Host 'Administrator account loaded. New databases require MEDTRUST_ADMIN_PASSWORD (12-128 characters).'
Write-Host "Stop with: powershell -ExecutionPolicy Bypass -File scripts\start_portal.ps1 stop"
