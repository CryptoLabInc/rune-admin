@echo off
setlocal

echo HiveMinded Installer Logic Wrapper
echo =================================
echo.

:ask_agent
echo Select your agent:
echo 1. Claude
echo 2. Gemini
echo 3. Codex
echo 4. Custom
echo.
set /p choice="Enter number (1-4): "

if "%choice%"=="1" set AGENT=claude
if "%choice%"=="2" set AGENT=gemini
if "%choice%"=="3" set AGENT=codex
if "%choice%"=="4" goto custom_agent
if not defined AGENT (
    echo Invalid choice.
    goto ask_agent
)

goto run_install

:custom_agent
set AGENT=custom
set /p INSTALL_DIR="Enter full installation path: "
goto run_install_custom

:run_install_custom
powershell -ExecutionPolicy Bypass -File "%~dp0install.ps1" -Agent %AGENT% -InstallDir "%INSTALL_DIR%"
goto end

:run_install
powershell -ExecutionPolicy Bypass -File "%~dp0install.ps1" -Agent %AGENT%

:end
echo.
if %ERRORLEVEL% EQU 0 (
    echo Installation finished. Press any key to close.
) else (
    echo Installation failed. Press any key to close.
)
pause >nul
