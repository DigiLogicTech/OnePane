# Optional Authenticode hook for OnePane Windows payloads.
# Signing is skipped unless ONEPANE_WINDOWS_SIGN_THUMBPRINT is set.
# The script never creates a certificate and never reads a private key from
# the command line. Point ONEPANE_WINDOWS_SIGNTOOL at signtool.exe when it is
# not already on PATH. ONEPANE_WINDOWS_TIMESTAMP_URL overrides the timestamp
# service.
param(
    [Parameter(Mandatory = $true)][string]$Path
)
$ErrorActionPreference = 'Stop'
if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) { throw "Signable file not found: $Path" }
if (-not $env:ONEPANE_WINDOWS_SIGN_THUMBPRINT) {
    Write-Output 'unsigned'
    exit 0
}
$tool = $env:ONEPANE_WINDOWS_SIGNTOOL
if (-not $tool) {
    $cmd = Get-Command signtool.exe -ErrorAction SilentlyContinue
    if ($cmd) { $tool = $cmd.Source }
}
if (-not $tool) { throw 'ONEPANE_WINDOWS_SIGN_THUMBPRINT is set but signtool.exe was not found. Set ONEPANE_WINDOWS_SIGNTOOL.' }
$timestamp = $env:ONEPANE_WINDOWS_TIMESTAMP_URL
if (-not $timestamp) { $timestamp = 'http://timestamp.digicert.com' }
& $tool sign /sha1 $env:ONEPANE_WINDOWS_SIGN_THUMBPRINT /fd SHA256 /tr $timestamp /td SHA256 $Path
if ($LASTEXITCODE -ne 0) { throw "signtool failed for $Path" }
Write-Output 'signed'
