@echo off
setlocal

echo Rune Interactive Installer
echo ==========================
echo.
echo Rune is an agent-agnostic organizational memory system.
echo It helps teams capture and retrieve context across any AI agent.
echo.

powershell -ExecutionPolicy Bypass -File "%~dp0install.ps1"

if %ERRORLEVEL% EQU 0 (
    echo.
    echo Installation complete. Press any key to close.
) else (
    echo.
    echo Installation failed. Press any key to close.
)

echo.
pause
