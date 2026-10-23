$ErrorActionPreference = 'Stop'
$savedApproval = $env:ONEPANE_WINDOWS_QA_REVIEWED_SHA
$output = Join-Path ([IO.Path]::GetTempPath()) ('onepane-unapproved-build-' + [guid]::NewGuid().ToString('N'))
try {
    $env:ONEPANE_WINDOWS_QA_REVIEWED_SHA = ''
    $sha = (& git rev-parse HEAD).Trim()
    if ($LASTEXITCODE -ne 0) { throw 'Git source identity unavailable' }
    $denied = $false
    try {
        & (Join-Path $PSScriptRoot 'build-rc11-windows-qa.ps1') -ExpectedCommitSha $sha -WixExecutable (Join-Path $output 'missing-wix.exe') -OutputDirectory $output
    } catch {
        if ($_.Exception.Message -notlike '*has not been approved for QA packaging*') { throw }
        $denied = $true
    }
    if (-not $denied -or (Test-Path -LiteralPath $output)) { throw 'Unapproved packaging was admitted or wrote an output directory' }
    Write-Output 'PASS: unapproved QA packaging fails before tool execution or artifact writes'
} finally {
    $env:ONEPANE_WINDOWS_QA_REVIEWED_SHA = $savedApproval
}
