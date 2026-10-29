# Read-only source fences shared by the guarded builder and disposable tests.
# Loading this file performs no build, approval, repository mutation or install.
function Assert-OnePaneWindowsQASource {
    param(
        [Parameter(Mandatory=$true)][string]$RepositoryRoot,
        [Parameter(Mandatory=$true)][ValidatePattern('^[a-f0-9]{40}$')][string]$ExpectedCommitSha,
        [string]$GeneratedOutputDirectory
    )
    $root = [IO.Path]::GetFullPath($RepositoryRoot).TrimEnd([IO.Path]::DirectorySeparatorChar)
    Push-Location -LiteralPath $root
    try {
        $top = (& git rev-parse --show-toplevel).Trim()
        if ($LASTEXITCODE -ne 0 -or -not [string]::Equals([IO.Path]::GetFullPath($top),$root,[StringComparison]::OrdinalIgnoreCase)) {
            throw 'Packaging must use the current repository root'
        }
        $actual = (& git rev-parse HEAD).Trim()
        if ($LASTEXITCODE -ne 0 -or $actual -cne $ExpectedCommitSha) { throw 'Checked-out source differs from approved SHA' }
        # Generated files may be untracked, but an index/working-tree change is
        # still a source change even if someone staged it under that directory.
        & git diff --quiet --exit-code -- .
        if ($LASTEXITCODE -ne 0) { throw 'Refusing to package a modified source tree' }
        & git diff --cached --quiet --exit-code -- .
        if ($LASTEXITCODE -ne 0) { throw 'Refusing to package a modified source tree' }
        $statusArguments = @('status','--porcelain','--untracked-files=all','--','.')
        if ($GeneratedOutputDirectory) {
            $output = [IO.Path]::GetFullPath($GeneratedOutputDirectory).TrimEnd([IO.Path]::DirectorySeparatorChar)
            if ([string]::Equals($output,$root,[StringComparison]::OrdinalIgnoreCase)) { throw 'Candidate output cannot be the source root' }
            $prefix = $root + [IO.Path]::DirectorySeparatorChar
            if ($output.StartsWith($prefix,[StringComparison]::OrdinalIgnoreCase)) {
                # The builder required this exact directory to be absent before
                # creating it. Only its generated untracked output is excluded;
                # literal pathspec semantics retain checks for sibling paths.
                $relative = $output.Substring($prefix.Length).Replace('\','/')
                $statusArguments += ':(exclude,literal)' + $relative
            }
        }
        $dirty = @(& git @statusArguments)
        if ($LASTEXITCODE -ne 0 -or $dirty.Count -ne 0) { throw 'Refusing to package a modified source tree' }
    } finally {
        Pop-Location
    }
}
