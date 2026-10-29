param(
    [Parameter(Mandatory=$true)][ValidatePattern('^[a-f0-9]{40}$')][string]$ExpectedCommitSha,
    [Parameter(Mandatory=$true)][string]$WixExecutable,
    [string]$OutputDirectory = 'dist/windows-rc11-qa'
)
$ErrorActionPreference = 'Stop'
if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) { throw 'Native Windows is required' }
if ($env:ONEPANE_WINDOWS_QA_REVIEWED_SHA -cne $ExpectedCommitSha) { throw 'This exact source SHA has not been approved for QA packaging' }
$repo = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
. (Join-Path $PSScriptRoot 'rc11-windows-qa-source.ps1')
Push-Location $repo
try {
    Assert-OnePaneWindowsQASource -RepositoryRoot $repo -ExpectedCommitSha $ExpectedCommitSha
    if (-not (Test-Path -LiteralPath $WixExecutable -PathType Leaf)) { throw 'Pinned WiX executable unavailable' }
    $wixVersion = (& $WixExecutable --version | Out-String).Trim()
    if ($LASTEXITCODE -ne 0 -or $wixVersion -notmatch '^5\.0\.2(?:[+.\s-]|$)') { throw 'WiX 5.0.2 is required; do not mislabel another compiler' }
    $go = (Get-Command go -ErrorAction Stop).Source
    $output = [IO.Path]::GetFullPath((Join-Path $repo $OutputDirectory))
    if (Test-Path -LiteralPath $output) { throw 'Candidate output already exists; do not overwrite a QA artifact' }
    $payload = Join-Path $output 'payload'
    New-Item -ItemType Directory -Path $payload -Force | Out-Null
    $version = '0.1.0-alpha.3.3-rc.11'
    # MSI ProductVersion is explicit. OnePane.wxs refuses to build without it.
    $productVersion = '3.3.11'
    if ([string]::IsNullOrWhiteSpace($productVersion)) { throw 'MSI ProductVersion is required' }
    $script:OnePaneSigningStatus = 'unsigned'
    $productName = 'DigiLogic OnePane Alpha 3.3 RC11 QA'
    $buildTime = [DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ')
    $flags = "-s -w -X github.com/DigiLogicTech/OnePane/internal/buildinfo.Version=$version -X github.com/DigiLogicTech/OnePane/internal/buildinfo.Revision=$ExpectedCommitSha -X github.com/DigiLogicTech/OnePane/internal/buildinfo.BuildTime=$buildTime"
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    $env:CGO_ENABLED = '0'
    $env:GOWORK = 'off'
    & $go mod verify
    if ($LASTEXITCODE -ne 0) { throw 'Pinned Go dependency verification failed' }
    foreach ($target in @(
        @{ Name='OnePane.Backend.exe'; Package='./cmd/harnessd'; GUI=$false },
        @{ Name='OnePane.Service.exe'; Package='./packaging/windows/service'; GUI=$false },
        @{ Name='OnePane.Desktop.exe'; Package='./packaging/windows/desktop'; GUI=$true },
        @{ Name='OnePane.Preflight.exe'; Package='./packaging/windows/preflight'; GUI=$false }
    )) {
        $targetFlags = $flags
        if ($target.GUI) { $targetFlags = '-H windowsgui ' + $flags }
        & $go build -trimpath -ldflags $targetFlags -o (Join-Path $payload $target.Name) $target.Package
        if ($LASTEXITCODE -ne 0) { throw "Compilation failed: $($target.Name)" }
        $signed = & (Join-Path $PSScriptRoot 'sign-windows-payload.ps1') -Path (Join-Path $payload $target.Name)
        if ($LASTEXITCODE -ne 0) { throw "Signing hook failed: $($target.Name)" }
        if ($signed -eq 'signed') { $script:OnePaneSigningStatus = 'signed' }
    }
    Copy-Item -LiteralPath (Join-Path $repo 'packaging/windows/assets/OnePane.ico') -Destination (Join-Path $payload 'OnePane.ico')
    $msi = Join-Path $output "OnePane-$version-windows-x64.msi"
    $authoring = Join-Path $repo 'packaging/windows/msi/OnePane.wxs'
    & $WixExecutable build $authoring -arch x64 -ext WixToolset.UI.wixext -ext WixToolset.Util.wixext -d "PayloadDir=$payload" -d "ProductName=$productName" -d "ProductVersion=$productVersion" -o $msi
    if ($LASTEXITCODE -ne 0 -or -not (Test-Path -LiteralPath $msi -PathType Leaf)) { throw 'Native MSI compilation failed' }
    $signedMsi = & (Join-Path $PSScriptRoot 'sign-windows-payload.ps1') -Path $msi
    if ($LASTEXITCODE -ne 0) { throw 'Signing hook failed for the MSI' }
    if ($signedMsi -eq 'signed') { $script:OnePaneSigningStatus = 'signed' }

    # Inspect the MSI database without installing, starting a service or touching
    # any operator data. A filename alone is not proof of ProductVersion.
    $installer = New-Object -ComObject WindowsInstaller.Installer
    $database = $installer.OpenDatabase($msi,0)
    foreach ($property in @(
        @{ Name='ProductVersion'; Expected=$productVersion },
        @{ Name='ProductName'; Expected=$productName },
        @{ Name='UpgradeCode'; Expected='{9F7D57AE-B2C4-44E2-96E4-F09E145AE235}' }
    )) {
        $view = $database.OpenView("SELECT ``Value`` FROM ``Property`` WHERE ``Property`` = '$($property.Name)'")
        $view.Execute()
        $record = $view.Fetch()
        if (-not $record -or $record.StringData(1) -ne $property.Expected) { throw "MSI identity mismatch: $($property.Name)" }
        $view.Close()
    }
    # Compilation and MSI assembly are slow boundaries. Do not stamp an
    # approved source identity onto output after the checkout has drifted.
    Assert-OnePaneWindowsQASource -RepositoryRoot $repo -ExpectedCommitSha $ExpectedCommitSha -GeneratedOutputDirectory $output
    $files = @((Get-ChildItem -LiteralPath $payload -File) + (Get-Item -LiteralPath $msi))
    $hashes = @($files | ForEach-Object { @{ name=$_.Name; sha256=(Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant(); bytes=$_.Length } })
    $manifest = @{
        schema_version=1; version=$version; product_version=$productVersion
        source_commit=$ExpectedCommitSha; build_time_utc=$buildTime
        workflow_run_id=$env:GITHUB_RUN_ID; workflow_attempt=$env:GITHUB_RUN_ATTEMPT
        integration_ci_run_id=$env:ONEPANE_SOURCE_CI_RUN_ID
        platform='windows-amd64'; wix_version='5.0.2'; signing_status=$script:OnePaneSigningStatus
        authoring_sha256=(Get-FileHash -LiteralPath $authoring -Algorithm SHA256).Hash.ToLowerInvariant()
        installed_acceptance='not_performed'; files=$hashes
    }
    $manifest | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath (Join-Path $output 'candidate-manifest.json') -Encoding utf8
    $msiHash = (Get-FileHash -LiteralPath $msi -Algorithm SHA256).Hash.ToLowerInvariant()
    "$msiHash  $([IO.Path]::GetFileName($msi))" | Set-Content -LiteralPath "$msi.sha256" -Encoding ascii
    @"
OnePane RC11 Windows x64 QA candidate
Signing: see signing_status in candidate-manifest.json. The signing hook is optional.
Source: $ExpectedCommitSha
MSI ProductVersion: $productVersion
Verify the MSI SHA-256 against its .sha256 and candidate-manifest.json.
This artifact has not been installed or qualified on your computer.
Back up the existing OnePane data/configuration/model/Project roots before QA.
Use the approved Windows-first install/upgrade/rollback checklist.
No automatic model, Node or Level 5 action is authorized by installation.
"@ | Set-Content -LiteralPath (Join-Path $output 'QA-README.txt') -Encoding utf8
    Assert-OnePaneWindowsQASource -RepositoryRoot $repo -ExpectedCommitSha $ExpectedCommitSha -GeneratedOutputDirectory $output
    Write-Output "QA candidate assembled from approved source $ExpectedCommitSha"
} finally {
    Pop-Location
}
