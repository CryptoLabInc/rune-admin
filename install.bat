@echo off
setlocal

echo.
echo ================================================
echo Rune-Vault Setup (Windows)
echo ================================================
echo.
echo Note: This script is for team administrators deploying Rune-Vault.
echo Team members don't need this - you'll receive a setup package.
echo.
echo This will check requirements and install Python dependencies.
echo.

set /p confirm="Continue with Vault setup? (y/N): "
if /i not "%confirm%"=="y" (
    echo Setup cancelled.
    exit /b 0
)

echo.
echo Starting setup...
echo.

powershell -ExecutionPolicy Bypass -File "%~dp0install.ps1"

if %ERRORLEVEL% EQU 0 (
    echo.
    echo Setup completed successfully!
) else (
    echo.
    echo Setup failed. Check the error messages above.
)

echo.
pause
