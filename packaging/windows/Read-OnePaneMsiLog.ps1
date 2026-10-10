#requires -Version 5.1
<#
.SYNOPSIS
  Extract only typed troubleshooting indicators from an operator-supplied
  Windows Installer log. No setup is launched; no files are written/uploaded.
.DESCRIPTION
  Read a pre-existing MSI log created by an operator with msiexec /L*v.
  Never include raw log lines, product paths, users, properties, commands,
  tokens, error payloads or service names in the JSON output.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][ValidateNotNullOrEmpty()][string] $LogPath
)
$ErrorActionPreference = 'Stop'

# Bound source input before reading. Raw installer logs are often sensitive.
$MaxLogBytes = 16MB
$MaxLines = 50000
$MaxLineLength = 8192

try {
    $item = Get-Item -LiteralPath $LogPath -Force -ErrorAction Stop
    if ($item.PSIsContainer -or (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0)) {
        throw 'invalid source type'
    }
    if ($item.Length -lt 0 -or $item.Length -gt $MaxLogBytes) {
        throw 'source exceeds read limit'
    }
} catch {
    throw 'Offline MSI diagnostic source unavailable, oversized or unsafe'
}

$actions = @('InstallFiles','InstallServices','StartServices','StopServices',
    'DeleteServices','WriteRegistryValues','RegisterProduct','PublishProduct',
    'InstallFinalize','InstallValidate','CostFinalize','MigrateFeatureStates',
    'RemoveExistingProducts','LaunchConditions')
$indicators = [ordered]@{
    engine_failure_marker = $false
    install_failure_text = $false
    access_denied = $false
    service_start_failure = $false
    registry_write_failure = $false
    payload_write_failure = $false
    custom_action_failure = $false
}
$lastAction = 'not_observed'
$engineCode = 'not_recorded'
$lineCount = 0
$readFailed = $false

try {
    # Stream source lines. Never accumulate or echo arbitrary log data.
    foreach ($line in [IO.File]::ReadLines($item.FullName)) {
        $lineCount++
        if ($lineCount -gt $MaxLines) { throw 'line limit' }
        if ($line.Length -gt $MaxLineLength) { throw 'line limit' }

        # Only exact fixed MSI action names are included in the report.
        if ($line -match 'Action start[^\r\n]{0,100}\b(InstallFiles|InstallServices|StartServices|StopServices|DeleteServices|WriteRegistryValues|RegisterProduct|PublishProduct|InstallFinalize|InstallValidate|CostFinalize|MigrateFeatureStates|RemoveExistingProducts|LaunchConditions)\b') {
            if ($actions -contains $Matches[1]) { $lastAction = $Matches[1] }
        }
        if ($line -match 'MainEngineThread is returning\s+(\d{1,5})\b') {
            switch ($Matches[1]) {
                '0'    { $engineCode = 'success' }
                '3010' { $engineCode = 'reboot_required' }
                '1603' { $engineCode = 'fatal_install_error' }
                '1618' { $engineCode = 'another_install_in_progress' }
                '1619' { $engineCode = 'package_not_accessible' }
                '1620' { $engineCode = 'invalid_package' }
                '1638' { $engineCode = 'existing_version_conflict' }
                default { $engineCode = 'other_or_unverified' }
            }
        }
        if ($line -match '\bReturn value 3\b') { $indicators.engine_failure_marker = $true }
        if ($line -match '\bInstallation failed\b') { $indicators.install_failure_text = $true }
        if ($line -match '\bError\s+(1303|1310|1321|1326|2203)\b') {
            $indicators.access_denied = $true
        }
        if ($line -match '\bError\s+(1920|1923|1921)\b') {
            $indicators.service_start_failure = $true
        }
        if ($line -match '\bError\s+(1402|1406)\b') {
            $indicators.registry_write_failure = $true
        }
        if ($line -match '\bError\s+(1305|1335|2350)\b') {
            $indicators.payload_write_failure = $true
        }
        if ($line -match '\bError\s+(1722|1723)\b') {
            $indicators.custom_action_failure = $true
        }
    }
} catch {
    $readFailed = $true
}

# Fail closed: a truncated/unreadable log is not a reliable diagnostic summary.
if ($readFailed) { throw 'Offline MSI log could not be fully and safely analysed' }

$failure = $indicators.engine_failure_marker -or $indicators.install_failure_text
$outcome = 'unknown'
if ($engineCode -eq 'success') { $outcome = 'reported_success' }
elseif ($engineCode -eq 'reboot_required') { $outcome = 'reported_reboot_required' }
elseif ($engineCode -in @('fatal_install_error','another_install_in_progress',
    'package_not_accessible','invalid_package','existing_version_conflict') -or $failure) {
    $outcome = 'reported_failure'
}

$report = [ordered]@{
    schema_version = 1
    source = 'operator_supplied_local_msi_log'
    observed_outcome = $outcome
    engine_result = $engineCode
    last_observed_known_action = $lastAction
    indicators = $indicators
    source_line_count = $lineCount
    evidence_limit = 'fixed MSI signatures only; not a verified root cause or sanitised original log'
}

# The only output consists of allowlisted literals, booleans and a bounded count.
$report | ConvertTo-Json -Depth 4 -Compress
