param(
    [ValidateSet('start', 'stop', 'status')]
    [string]$Action = 'start'
)

$ErrorActionPreference = 'Stop'
$ProjectRoot = Split-Path -Parent $PSScriptRoot
$Binary = Join-Path $ProjectRoot 'medtrust-node.exe'
$LogDir = Join-Path $ProjectRoot 'logs'
$PidFile = Join-Path $LogDir 'release-cluster.pids'

function Get-OwnedProcesses {
    if (-not (Test-Path -LiteralPath $PidFile)) { return @() }
    $items = @()
    Get-Content -LiteralPath $PidFile | ForEach-Object {
        $processId = 0
        if ([int]::TryParse($_, [ref]$processId)) {
            $process = Get-Process -Id $processId -ErrorAction SilentlyContinue
            if ($null -ne $process -and $process.Path -eq $Binary) { $items += $process }
        }
    }
    return $items
}

if ($Action -eq 'stop') {
    Get-OwnedProcesses | ForEach-Object { Stop-Process -Id $_.Id }
    Remove-Item -LiteralPath $PidFile -Force -ErrorAction SilentlyContinue
    Write-Host 'MedTrust cluster stopped.'
    exit 0
}

if ($Action -eq 'status') {
    1..3 | ForEach-Object {
        $nodeNumber = $_
        $port = 8000 + $nodeNumber
        try {
            $health = Invoke-RestMethod -Uri "http://127.0.0.1:$port/health" -TimeoutSec 2
            Write-Host "node-$nodeNumber  port=$port  status=$($health.status)"
        } catch {
            Write-Host "node-$nodeNumber  port=$port  status=offline"
        }
    }
    exit 0
}

if (-not (Test-Path -LiteralPath $Binary)) {
    throw 'medtrust-node.exe is missing. Use the complete release package or build the program first.'
}
if (-not (Test-Path -LiteralPath (Join-Path $ProjectRoot 'web\dist\index.html'))) {
    throw 'The built frontend is missing from web\dist.'
}
if ((Get-OwnedProcesses).Count -gt 0) {
    Write-Host 'MedTrust cluster is already running.'
    exit 0
}

Set-Location $ProjectRoot
New-Item -ItemType Directory -Force -Path $LogDir | Out-Null
$started = @()
try {
    1..3 | ForEach-Object {
        $config = Join-Path $ProjectRoot "configs\node$_.yaml"
        $started += Start-Process -FilePath $Binary -ArgumentList @('--config', $config) -PassThru -WindowStyle Hidden
    }
    $started.Id | Set-Content -LiteralPath $PidFile
    foreach ($port in 8001..8003) {
        $ready = $false
        for ($attempt = 0; $attempt -lt 20; $attempt++) {
            try {
                $health = Invoke-RestMethod -Uri "http://127.0.0.1:$port/health" -TimeoutSec 1
                if ($health.status -eq 'healthy') { $ready = $true; break }
            } catch { Start-Sleep -Milliseconds 500 }
        }
        if (-not $ready) { throw "Node on port $port did not become healthy." }
    }
} catch {
    $started | ForEach-Object { Stop-Process -Id $_.Id -ErrorAction SilentlyContinue }
    Remove-Item -LiteralPath $PidFile -Force -ErrorAction SilentlyContinue
    throw
}

Write-Host 'MedTrust is running at http://127.0.0.1:8001'
Write-Host 'Administrator account loaded. New databases require MEDTRUST_ADMIN_PASSWORD (12-128 characters).'
Write-Host 'Patient: securep164624 / Medtrust123'
Write-Host 'Doctor: secured164624 / Medtrust123'
Write-Host 'Stop with: powershell -ExecutionPolicy Bypass -File scripts\start_release.ps1 stop'
