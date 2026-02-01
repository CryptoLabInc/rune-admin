<#
.SYNOPSIS
HiveMinded Agent-Agnostic Installer for Windows

.DESCRIPTION
Installs skills for Claude, Gemini, Codex, or custom agents. This is the Windows equivalent of install.sh.

.PARAMETER Agent
The agent type: claude, gemini, codex, or custom.

.PARAMETER InstallDir
Custom installation directory. Required for 'custom' agent, optional for others.

.EXAMPLE
.\install.ps1 -Agent claude
.\install.ps1 -Agent gemini -InstallDir "C:\MySkills"
#>

param(
    [Parameter(Mandatory = $true)]
    [ValidateSet("claude", "gemini", "codex", "custom")]
    [string]$Agent,

    [Parameter(Mandatory = $false)]
    [string]$InstallDir
)

$ErrorActionPreference = "Stop"

# -- Colors for Output --
function Write-Info ($Message) { Write-Host "[INFO] $Message" -ForegroundColor Green }
function Write-Warn ($Message) { Write-Host "[WARN] $Message" -ForegroundColor Yellow }
function Write-ErrorMsg ($Message) { Write-Host "[ERROR] $Message" -ForegroundColor Red }

# -- Main Logic --

try {
    Write-Info "HiveMinded Windows Installer"
    Write-Info "Agent: $Agent"

    # 1. Detect Install Directory
    if ([string]::IsNullOrWhiteSpace($InstallDir)) {
        switch ($Agent) {
            "claude" {
                if (Test-Path "$env:APPDATA\Claude") {
                    $InstallDir = "$env:APPDATA\Claude\skills"
                }
                elseif (Test-Path "$env:USERPROFILE\.claude") {
                    $InstallDir = "$env:USERPROFILE\.claude\skills"
                }
                else {
                    $InstallDir = "$env:APPDATA\Claude\skills" # Default fallback
                }
            }
            "gemini" {
                $InstallDir = "$env:USERPROFILE\.gemini\skills"
            }
            "codex" {
                $InstallDir = "$env:USERPROFILE\.codex\skills"
            }
            "custom" {
                Write-ErrorMsg "Custom agent requires -InstallDir parameter."
                exit 1
            }
        }
    }

    Write-Info "Installing to: $InstallDir"

    # 2. Create Directory
    if (-not (Test-Path $InstallDir)) {
        New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
    }

    # 3. Copy Skills
    $SourcePath = Join-Path $PSScriptRoot "skills\envector"
    
    if (-not (Test-Path $SourcePath)) {
        throw "Source skills not found at $SourcePath. Make sure you unzip the entire package."
    }

    Write-Info "Installing enVector skill..."
    Copy-Item -Path $SourcePath -Destination $InstallDir -Recurse -Force

    Write-Info "Skills installed successfully!"

    # 4. Create Config
    Write-Info "Creating configuration..."
    $ConfigPath = Join-Path (Split-Path $InstallDir -Parent) "config.json"
    
    $ConfigObj = @{
        skills = @{
            envector = @{
                enabled     = $true
                mcp_servers = @{
                    vault = @{
                        url   = '${VAULT_URL}'
                        token = '${VAULT_TOKEN}'
                    }
                }
            }
        }
    }

    $JsonContent = $ConfigObj | ConvertTo-Json -Depth 5
    Set-Content -Path $ConfigPath -Value $JsonContent

    Write-Info "Config created at: $ConfigPath"

    # 5. Success Message
    Write-Host ""
    Write-Info "[OK] HiveMinded installed successfully!"
    Write-Host ""
    Write-Host "Next steps:"
    Write-Host "1. Configure environment variables (System Properties -> Environment Variables):"
    Write-Host "   VAULT_URL = 'https://vault-your-team.oci.envector.io'" -ForegroundColor Cyan
    Write-Host "   VAULT_TOKEN = 'evt_xxx'" -ForegroundColor Cyan
    Write-Host ""
    Write-Host "2. Restart your agent to load the new skills."
    Write-Host ""

}
catch {
    Write-ErrorMsg "An error occurred: $_"
    exit 1
}
