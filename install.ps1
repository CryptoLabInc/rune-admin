<#
.SYNOPSIS
Rune Interactive Installer for Windows

.DESCRIPTION
Non-developer friendly setup for Rune organizational memory system.
Guides you through role-based installation (Admin or Team Member).

.EXAMPLE
.\install.ps1
#>

$ErrorActionPreference = "Stop"
$Version = "0.2.0"

# -- Colors for Output --
function Write-Info ($Message) { Write-Host "✓ $Message" -ForegroundColor Green }
function Write-Warn ($Message) { Write-Host "⚠ $Message" -ForegroundColor Yellow }
function Write-ErrorMsg ($Message) { Write-Host "✗ $Message" -ForegroundColor Red }
function Write-Header ($Message) { 
    Write-Host "`n================================================" -ForegroundColor Blue
    Write-Host $Message -ForegroundColor Blue
    Write-Host "================================================`n" -ForegroundColor Blue
}
function Write-Step ($Message) {
    Write-Host "`n▸ $Message`n" -ForegroundColor Blue
}

# -- Main Logic --

function Test-Python {
    try {
        $pythonVersion = python --version 2>&1
        if ($pythonVersion -match "Python (\d+\.\d+\.\d+)") {
            Write-Info "Python $($matches[1]) detected"
            return $true
        }
    }
    catch {
        Write-ErrorMsg "Python is not installed"
        Write-Host "Please install Python 3.8 or higher from:"
        Write-Host "  https://www.python.org/downloads/"
        return $false
    }
}

function Setup-VaultDependencies {
    Write-Step "Setting up Rune-Vault dependencies..."
    
    Set-Location "mcp\vault"
    
    # Create virtual environment
    if (-not (Test-Path ".venv")) {
        Write-Info "Creating Python virtual environment..."
        python -m venv .venv
    }
    else {
        Write-Info "Virtual environment already exists"
    }
    
    # Activate venv
    & ".venv\Scripts\Activate.ps1"
    
    # Install dependencies
    Write-Info "Installing Python packages (this may take a few minutes)..."
    python -m pip install --quiet --upgrade pip
    python -m pip install --quiet pyenvector fastmcp psutil prometheus-client
    
    Write-Info "Dependencies installed successfully!"
    
    Set-Location "..\..\"
}

function Show-AdminNextSteps {
    Write-Header "Setup Complete! Next Steps for Admin"
    
    Write-Host "1️⃣  Deploy Rune-Vault to cloud:"
    Write-Host "   cd deployment\oci    # or aws, gcp"
    Write-Host "   terraform init"
    Write-Host "   terraform apply"
    Write-Host ""
    Write-Host "2️⃣  Share with team members:"
    Write-Host "   - Vault URL: https://vault-YOURTEAM.oci.envector.io"
    Write-Host "   - Vault Token: evt_YOURTEAM_xxx"
    Write-Host ""
    Write-Host "3️⃣  Onboard team members:"
    Write-Host "   .\scripts\add-team-member.sh"
    Write-Host ""
    Write-Host "📚 Deployment Guide: deployment\oci\README.md"
    Write-Host "💬 Support: https://github.com/CryptoLabInc/rune/issues"
    Write-Host ""
}

function Show-MemberNextSteps {
    Write-Header "Setup Complete! Next Steps for Team Member"
    
    Write-Host "Wait for your admin to send you:"
    Write-Host "  1. Vault URL (https://vault-YOURTEAM.oci.envector.io)"
    Write-Host "  2. Vault Token (evt_YOURTEAM_xxx)"
    Write-Host "  3. Setup package (YOURNAME_rune_package.zip)"
    Write-Host ""
    Write-Host "Once received:"
    Write-Host "  1. Extract the package"
    Write-Host "  2. Run the setup script inside"
    Write-Host "  3. Restart your AI agent"
    Write-Host ""
    Write-Host "📚 Configuration Guide: CLAUDE_SETUP.md"
    Write-Host "💬 Support: https://github.com/CryptoLabInc/rune/issues"
    Write-Host ""
}

# Main interactive installation
try {
    Write-Header "Rune Interactive Installer v$Version"
    
    Write-Host "Rune is an agent-agnostic organizational memory system."
    Write-Host "It helps teams capture and retrieve context across any AI agent."
    Write-Host ""
    
    Write-Step "What's your role?"
    Write-Host "1) Team Admin (will deploy Rune-Vault)"
    Write-Host "2) Team Member (will connect to existing Vault)"
    Write-Host ""
    
    $role = Read-Host "Select (1 or 2)"
    
    switch ($role) {
        "1" {
            Write-Header "Admin Setup"
            
            if (-not (Test-Python)) {
                exit 1
            }
            
            Setup-VaultDependencies
            
            Write-Info "Admin setup complete!"
            Show-AdminNextSteps
        }
        "2" {
            Write-Header "Team Member Setup"
            
            Write-Host "As a team member, you don't need to install anything locally."
            Write-Host "Your admin will provide you with a setup package."
            Write-Host ""
            
            Write-Info "No installation needed!"
            Show-MemberNextSteps
        }
        default {
            Write-ErrorMsg "Invalid selection. Please run the script again."
            exit 1
        }
    }
    
    Write-Info "Setup complete! 🎉"
}
catch {
    Write-ErrorMsg "An error occurred: $_"
    exit 1
}
