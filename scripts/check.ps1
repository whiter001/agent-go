[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repoRoot = Split-Path -Parent $PSScriptRoot

Push-Location $repoRoot
try {
    $goFiles = Get-ChildItem -Recurse -Filter *.go | ForEach-Object { $_.FullName }
    $unformatted = & gofmt -l @goFiles
    if ($unformatted) {
        Write-Error 'Go files need formatting:'
        $unformatted | ForEach-Object { Write-Error $_ }
        exit 1
    }

    go vet ./...
    go test ./...
    go build ./...

    Write-Host 'Check passed.'
}
finally {
    Pop-Location
}
