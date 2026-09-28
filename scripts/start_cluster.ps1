[CmdletBinding()]
param(
    [ValidateSet('start', 'stop', 'status', 'reset')]
    [string]$Action = 'start'
)

$ErrorActionPreference = 'Stop'

$RepoRoot = Split-Path -Parent $PSScriptRoot
$Binary = Join-Path $RepoRoot 'medtrust-node.exe'
$LogDir = Join-Path $PSScriptRoot 'logs'
$GoCache = Join-Path $RepoRoot '.gocache'

$Nodes = @(
    @{
        Name   = 'node-1'
        Config = Join-Path $RepoRoot 'configs\node1.yaml'
        DBPath = Join-Path $RepoRoot 'data\node-1\chain.db'
        Log    = Join-Path $LogDir 'node1.log'
        API    = 'http://127.0.0.1:8001/api/node/status'
    },
    @{
        Name   = 'node-2'
        Config = Join-Path $RepoRoot 'configs\node2.yaml'
        DBPath = Join-Path $RepoRoot 'data\node-2\chain.db'
        Log    = Join-Path $LogDir 'node2.log'
        API    = 'http://127.0.0.1:8002/api/node/status'
    },
    @{
        Name   = 'node-3'
        Config = Join-Path $RepoRoot 'configs\node3.yaml'
        DBPath = Join-Path $RepoRoot 'data\node-3\chain.db'
        Log    = Join-Path $LogDir 'node3.log'
        API    = 'http://127.0.0.1:8003/api/node/status'
    }
)

function Ensure-Dir {
    param([string]$Path)
    New-Item -ItemType Directory -Force -Path $Path | Out-Null
}

function Build-Binary {
    Ensure-Dir $GoCache
    $env:GOCACHE = $GoCache
    Write-Host "[BUILD] compiling medtrust-node.exe"
    & go build -o $Binary .\cmd\node\
    if ($LASTEXITCODE -ne 0) {
        throw "go build failed"
    }
}

function Start-Node {
    param([hashtable]$Node)

    Ensure-Dir (Split-Path -Parent $Node.DBPath)
    Ensure-Dir $LogDir

    if (Test-Path $Node.Log) {
        Remove-Item -LiteralPath $Node.Log -Force
    }

    $cmdArgs = '/c start "{0}" /b "{1}" --config "{2}" 1>>"{3}" 2>>&1' -f $Node.Name, $Binary, $Node.Config, $Node.Log

    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = 'C:\Windows\System32\cmd.exe'
    $psi.Arguments = $cmdArgs
    $psi.WorkingDirectory = $RepoRoot
    $psi.UseShellExecute = $false
    $psi.CreateNoWindow = $true

    $launcher = [System.Diagnostics.Process]::Start($psi)
    if ($null -eq $launcher) {
        throw "failed to start $($Node.Name)"
    }
    $launcher.WaitForExit()

    for ($attempt = 0; $attempt -lt 30; $attempt++) {
        Start-Sleep -Milliseconds 250
        $running = Get-Process -Name 'medtrust-node' -ErrorAction SilentlyContinue
        if ($running) {
            return
        }
    }

    throw "failed to start $($Node.Name)"
}

function Stop-Cluster {
    $procs = Get-Process -Name 'medtrust-node' -ErrorAction SilentlyContinue |
        Sort-Object Id -Unique

    foreach ($proc in $procs) {
        Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue
        Write-Host "[STOP] pid=$($proc.Id)"
    }
}

function Reset-ClusterData {
    foreach ($node in $Nodes) {
        $dbPath = [System.IO.Path]::GetFullPath($node.DBPath)
        $dataRoot = [System.IO.Path]::GetFullPath((Split-Path -Parent $node.DBPath))
        if (-not $dbPath.StartsWith($dataRoot, [System.StringComparison]::OrdinalIgnoreCase)) {
            throw "refusing to remove unexpected path: $dbPath"
        }
        if (Test-Path $node.DBPath) {
            Remove-Item -LiteralPath $node.DBPath -Force
            Write-Host "[RESET] removed $($node.DBPath)"
        }
        if (Test-Path $node.Log) {
            Remove-Item -LiteralPath $node.Log -Force
        }
    }
}

function Show-Status {
    foreach ($node in $Nodes) {
        try {
            $response = Invoke-WebRequest -Uri $node.API -UseBasicParsing -TimeoutSec 2
            Write-Host ("[RUNNING] {0} {1}" -f $node.Name, $response.StatusCode)
        } catch {
            $proc = Get-Process -Name 'medtrust-node' -ErrorAction SilentlyContinue
            if ($proc) {
                Write-Host ("[STARTING] {0}" -f $node.Name)
            } else {
                Write-Host ("[STOPPED] {0}" -f $node.Name)
            }
            if (Test-Path $node.Log) {
                $tail = Get-Content -Path $node.Log -Tail 3 -ErrorAction SilentlyContinue
                foreach ($line in $tail) {
                    Write-Host ("          {0}" -f $line)
                }
            }
        }
    }
}

function Wait-ForClusterStart {
    foreach ($node in $Nodes) {
        for ($attempt = 0; $attempt -lt 20; $attempt++) {
            try {
                $null = Invoke-WebRequest -Uri $node.API -UseBasicParsing -TimeoutSec 2
                break
            } catch {
                Start-Sleep -Milliseconds 300
            }
        }
        $ready = $false
        try {
            $null = Invoke-WebRequest -Uri $node.API -UseBasicParsing -TimeoutSec 2
            $ready = $true
        } catch {
            $ready = $false
        }
        if ($ready) {
            Write-Host ("[READY] {0}" -f $node.Name)
        } else {
            throw "node API not reachable: $($node.API)"
        }
    }
}

switch ($Action) {
    'start' {
        Stop-Cluster
        Build-Binary
        Ensure-Dir $LogDir

        foreach ($node in $Nodes) {
            Start-Node $node
            Write-Host ("[START] {0}" -f $node.Name)
        }

        Wait-ForClusterStart
        Write-Host ""
        Write-Host "Cluster started."
        Write-Host "APIs: http://127.0.0.1:8001  http://127.0.0.1:8002  http://127.0.0.1:8003"
    }
    'stop' {
        Stop-Cluster
        Write-Host "Cluster stopped."
    }
    'status' {
        Show-Status
    }
    'reset' {
        Stop-Cluster
        Reset-ClusterData
        Write-Host "Cluster data reset."
    }
}
