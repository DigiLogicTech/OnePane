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

# Source/provenance checks use only a disposable repository. They do not set a
# reviewed-SHA declaration or invoke Go, WiX, Windows Installer or a service.
. (Join-Path $PSScriptRoot 'rc11-windows-qa-source.ps1')
$fixture = Join-Path ([IO.Path]::GetTempPath()) ('onepane-qa-source-fence-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $fixture | Out-Null
function Assert-SourceFenceDenies {
    param([string]$Root,[string]$Sha,[string]$GeneratedOutput,[string]$Reason)
    $rejected = $false
    try { Assert-OnePaneWindowsQASource -RepositoryRoot $Root -ExpectedCommitSha $Sha -GeneratedOutputDirectory $GeneratedOutput } catch { $rejected = $true }
    if (-not $rejected) { throw "Source fence accepted $Reason" }
}
Push-Location -LiteralPath $fixture
try {
    & git init --quiet
    if ($LASTEXITCODE -ne 0) { throw 'Disposable Git initialization failed' }
    $emptyHooks = Join-Path $fixture 'empty-test-hooks'
    New-Item -ItemType Directory -Path $emptyHooks | Out-Null
    $source = Join-Path $fixture 'source.txt'
    [IO.File]::WriteAllText($source,"original`n")
    & git add -- source.txt
    if ($LASTEXITCODE -ne 0) { throw 'Disposable source staging failed' }
    & git -c "core.hooksPath=$emptyHooks" -c commit.gpgsign=false -c user.name=OnePane-QA-Test -c user.email=qa-test@example.invalid commit --quiet -m fixture
    if ($LASTEXITCODE -ne 0) { throw 'Disposable source commit failed' }
    $expected = (& git rev-parse HEAD).Trim()
    if ($LASTEXITCODE -ne 0) { throw 'Disposable source identity unavailable' }
    Assert-OnePaneWindowsQASource -RepositoryRoot $fixture -ExpectedCommitSha $expected

    $generated = Join-Path (Join-Path $fixture 'generated QA [candidate]') 'windows-rc11-qa'
    New-Item -ItemType Directory -Path $generated | Out-Null
    [IO.File]::WriteAllText((Join-Path $generated 'test-output.txt'),"disposable generated marker`n")
    Assert-SourceFenceDenies -Root $fixture -Sha $expected -Reason 'untracked output before assembly'
    Assert-OnePaneWindowsQASource -RepositoryRoot $fixture -ExpectedCommitSha $expected -GeneratedOutputDirectory $generated
    & git add -- ':(literal)generated QA [candidate]/windows-rc11-qa/test-output.txt'
    if ($LASTEXITCODE -ne 0) { throw 'Disposable generated marker staging failed' }
    Assert-SourceFenceDenies -Root $fixture -Sha $expected -GeneratedOutput $generated -Reason 'staged content hidden inside generated output'
    & git restore --staged -- ':(literal)generated QA [candidate]/windows-rc11-qa/test-output.txt'
    if ($LASTEXITCODE -ne 0) { throw 'Disposable generated marker unstaging failed' }

    [IO.File]::WriteAllText($source,"changed during assembly`n")
    Assert-SourceFenceDenies -Root $fixture -Sha $expected -GeneratedOutput $generated -Reason 'tracked source modified during assembly'
    & git add -- source.txt
    if ($LASTEXITCODE -ne 0) { throw 'Disposable changed source staging failed' }
    Assert-SourceFenceDenies -Root $fixture -Sha $expected -GeneratedOutput $generated -Reason 'staged source modified during assembly'
    & git -c "core.hooksPath=$emptyHooks" -c commit.gpgsign=false -c user.name=OnePane-QA-Test -c user.email=qa-test@example.invalid commit --quiet -m changed
    if ($LASTEXITCODE -ne 0) { throw 'Disposable changed source commit failed' }
    Assert-SourceFenceDenies -Root $fixture -Sha $expected -GeneratedOutput $generated -Reason 'HEAD changed during assembly'
    $current = (& git rev-parse HEAD).Trim()
    if ($LASTEXITCODE -ne 0) { throw 'Disposable changed identity unavailable' }
    Assert-OnePaneWindowsQASource -RepositoryRoot $fixture -ExpectedCommitSha $current -GeneratedOutputDirectory $generated

    $sibling = Join-Path (Join-Path $fixture 'generated QA [candidate]') 'windows-rc11-qa-outside.txt'
    [IO.File]::WriteAllText($sibling,"untracked source outside the exact output directory`n")
    Assert-SourceFenceDenies -Root $fixture -Sha $current -GeneratedOutput $generated -Reason 'a same-prefix sibling outside generated output'
    Remove-Item -LiteralPath $sibling
    $untracked = Join-Path $fixture 'new-source.go'
    [IO.File]::WriteAllText($untracked,"package unexpected`n")
    Assert-SourceFenceDenies -Root $fixture -Sha $current -GeneratedOutput $generated -Reason 'untracked source introduced during assembly'
    Remove-Item -LiteralPath $untracked
    Assert-SourceFenceDenies -Root $fixture -Sha $current -GeneratedOutput $fixture -Reason 'source-root output exclusion'
    $nested = Join-Path $fixture 'nested'
    New-Item -ItemType Directory -Path $nested | Out-Null
    Assert-SourceFenceDenies -Root $nested -Sha $current -GeneratedOutput $generated -Reason 'a non-root repository directory'
    Write-Output 'PASS: exact-source fences reject source/HEAD drift and exclude only the generated output directory'
} finally {
    Pop-Location
    # Resolve and verify the exact directory created above before recursive
    # cleanup. No source checkout or installed application path is removed.
    $resolved = (Resolve-Path -LiteralPath $fixture).Path
    $expectedFixture = [IO.Path]::GetFullPath($fixture)
    $temporaryPrefix = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar
    if (-not [string]::Equals($resolved,$expectedFixture,[StringComparison]::OrdinalIgnoreCase) -or -not $resolved.StartsWith($temporaryPrefix,[StringComparison]::OrdinalIgnoreCase)) {
        throw 'Refusing cleanup outside the exact disposable source-fence fixture'
    }
    Remove-Item -LiteralPath $resolved -Recurse -Force
}
