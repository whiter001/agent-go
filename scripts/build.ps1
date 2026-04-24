[CmdletBinding()]
param(
    [string]$Output = 'bin/agent-go.exe'
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repoRoot = Split-Path -Parent $PSScriptRoot

if ([System.IO.Path]::IsPathRooted($Output)) {
    $outputPath = $Output
}
else {
    $outputPath = Join-Path $repoRoot $Output
}

$parentDir = Split-Path -Parent $outputPath
if ($parentDir) {
    New-Item -ItemType Directory -Path $parentDir -Force | Out-Null
}

Push-Location $repoRoot
try {
    go build -trimpath -o $outputPath .
    Write-Host "Built $outputPath"
}
finally {
    Pop-Location
}
