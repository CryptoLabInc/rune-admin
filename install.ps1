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

Team members don't need to run this - you'll receive an onboarding
package from your admin with a ready-to-use setup script.

This will:
  1. Check system requirements (Python, Docker, Terraform)
  2. Install Python dependencies for Vault
  3. Prepare vault keys directory
  4. Show next steps for deployment

"@ -ForegroundColor Yellow

$response = Read-Host "Continue with Vault setup? (y/N)"
if ($response -notmatch '^[Yy]$') {
    Write-Host "Setup cancelled."
    exit 0
}

# Step 1: Check System Requirements
Write-Step "Step 1: Checking System Requirements"

# Check Python
$pythonFound = $false
$pythonVersion = $null

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

Write-Host ""
Write-Host "Keys will be generated automatically on first Vault startup." -ForegroundColor Cyan
Write-Host "Keep the vault_keys/ directory SECURE and BACKED UP." -ForegroundColor Cyan

# Step 4: Next Steps
Write-Step "Setup Complete!"

Write-Host @"

✓ Python environment configured
✓ Dependencies installed
✓ Vault keys directory prepared

Next Steps:
============

Option A: Deploy to Cloud
--------------------------
1. Choose your provider:
   cd deployment/oci     # or aws, gcp
   
2. Configure Terraform variables:
   Edit terraform.tfvars with your settings
   
3. Deploy:
   terraform init
   terraform plan
   terraform apply
   
4. Note the Vault URL from outputs

Option B: Test Locally
----------------------
1. Generate keys and start Vault:
   cd mcp\vault
   .\run_vault.sh    # or use Docker directly
   
2. Vault will run at http://localhost:8000

Option C: Onboard Team Members
-------------------------------
After deploying Vault:

1. Add team member:
   .\scripts\add-team-member.sh alice

2. Share the generated setup package
   (team-setup-alice.zip)

3. Member runs setup script from package
   (no manual configuration needed)

Documentation:
--------------
- Quick Start: README.md
- Architecture: docs/ARCHITECTURE.md
- Team Setup: docs/TEAM-SETUP.md

"@ -ForegroundColor Green

Write-Host ""
Write-Host "Questions? See README.md or open an issue." -ForegroundColor Blue
Write-Host ""
