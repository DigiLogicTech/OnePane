$ErrorActionPreference = 'Stop'
$fail = $false
function Check($name, [scriptblock]$test) {
  try {
    $ok = & $test
    if ($ok) { Write-Host "[PASS] $name" -ForegroundColor Green }
    else { Write-Host "[FAIL] $name" -ForegroundColor Red; $script:fail = $true }
  } catch {
    Write-Host "[FAIL] $name - $($_.Exception.Message)" -ForegroundColor Red
    $script:fail = $true
  }
}

Write-Host "OnePane Windows alpha verification" -ForegroundColor Cyan
Write-Host "=================================" -ForegroundColor Cyan
Check 'OnePane Windows Service exists' { $null -ne (Get-Service -Name 'OnePane' -ErrorAction SilentlyContinue) }
Check 'OnePane Windows Service is running' { (Get-Service -Name 'OnePane').Status -eq 'Running' }
Check 'Backend installed' { Test-Path "$env:ProgramFiles\OnePane\OnePane.Backend.exe" }
Check 'Desktop installed' { Test-Path "$env:ProgramFiles\OnePane\OnePane.Desktop.exe" }
Check 'Service host installed' { Test-Path "$env:ProgramFiles\OnePane\OnePane.Service.exe" }
Check 'OnePane icon installed' { Test-Path "$env:ProgramFiles\OnePane\OnePane.ico" }
Check 'Control-plane health endpoint responds' {
  $r = Invoke-WebRequest -UseBasicParsing -TimeoutSec 5 'http://127.0.0.1:18181/v1/health'
  $r.StatusCode -ge 200 -and $r.StatusCode -lt 500
}
Check 'OnePane data directory exists' { Test-Path "$env:ProgramData\OnePane" }
Check 'Model pool is configured' {
  $cfg = Get-Content "$env:ProgramData\OnePane\config.yaml" -Raw
  $cfg -match '(?m)^\s*model_pool_path:\s*.+$'
}
if ($fail) {
  Write-Host "`nOne or more checks failed." -ForegroundColor Yellow
  Write-Host "Backend log: $env:ProgramData\OnePane\logs\onepane.log"
  Write-Host "Desktop log: $env:LOCALAPPDATA\OnePane\desktop.log"
  exit 1
}
Write-Host "`nOnePane Windows alpha smoke test passed." -ForegroundColor Green
