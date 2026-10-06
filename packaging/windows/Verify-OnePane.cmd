@echo off
setlocal
cd /d "%~dp0"
echo Running OnePane verification...
echo.
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0Verify-OnePane.ps1"
set "rc=%ERRORLEVEL%"
echo.
if not "%rc%"=="0" echo Verification reported one or more failures.
echo Press any key to close.
pause >nul
exit /b %rc%
