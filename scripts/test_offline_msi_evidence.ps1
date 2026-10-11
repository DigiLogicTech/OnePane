#requires -Version 5.1
$ErrorActionPreference = 'Stop'
$script = Join-Path $PSScriptRoot '..\packaging\windows\Read-OnePaneMsiLog.ps1'
$root = Join-Path ([IO.Path]::GetTempPath()) ('onepane-msi-qa-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $root | Out-Null
try {
    $canary = 'PRIVATE_MSI_PATH_USER_TOKEN_PASSWORD_CANARY'
    $msi = Join-Path $root 'msi.log'
    @"
MSI (s): Action start 00:12:00: InstallFiles.
MSI (s): Action start 00:12:02: StartServices.
MSI (s): Error 1920. Service $canary could not start.
MSI (s): $canary Return value 3.
MSI (s): MainEngineThread is returning 1603
MSI (s): Product: $canary -- Installation failed.
"@ | Set-Content -LiteralPath $msi -Encoding UTF8

    $json = & $script -LogPath $msi
    if (-not $json) { throw 'no report produced' }
    $report = $json | ConvertFrom-Json
    if ($report.schema_version -ne 1 -or $report.engine_result -ne 'fatal_install_error') {
        throw 'MSI exit category was not mapped'
    }
    if ($report.observed_outcome -ne 'reported_failure' -or
        $report.last_observed_known_action -ne 'StartServices' -or
        -not $report.indicators.service_start_failure -or
        -not $report.indicators.engine_failure_marker) {
        throw 'Expected fixed diagnostic categories absent'
    }
    if ($json -match $canary -or $json -match [regex]::Escape($root)) {
        throw 'Raw MSI private content leaked into JSON'
    }
    $fields = @($report.PSObject.Properties.Name)
    foreach ($field in $fields) {
        if ($field -notin @('schema_version','source','observed_outcome','engine_result',
            'last_observed_known_action','indicators','source_line_count','evidence_limit')) {
            throw 'Unexpected exported field'
        }
    }

    @"
MSI (s): Action start 00:10:00: InstallFinalize.
MSI (s): MainEngineThread is returning 3010
"@ | Set-Content -LiteralPath $msi -Encoding UTF8
    $reboot = (& $script -LogPath $msi) | ConvertFrom-Json
    if ($reboot.engine_result -ne 'reboot_required' -or
        $reboot.observed_outcome -ne 'reported_reboot_required') {
        throw 'MSI reboot-required outcome not recognised'
    }

    ('A' * 9000) | Set-Content -LiteralPath $msi -Encoding UTF8
    $rejected = $false
    try { $null = & $script -LogPath $msi } catch { $rejected = $true }
    if (-not $rejected) { throw 'Oversized source line was accepted' }
    Write-Host 'PASS: synthetic MSI categories, source caps, and secret-free offline JSON'
} finally {
    Remove-Item -LiteralPath $root -Force -Recurse -ErrorAction SilentlyContinue
}
