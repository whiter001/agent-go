[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repoRoot = Split-Path -Parent $PSScriptRoot

Push-Location $repoRoot
try {
    go fmt ./...
    Write-Host 'Formatted Go packages.'
}
finally {
    Pop-Location
}
