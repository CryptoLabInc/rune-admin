<#
.SYNOPSIS
Rune-Vault Setup Script for Windows

.DESCRIPTION
Prepares environment for deploying Rune-Vault (admins only).
Team members don't need to run this - use onboarding package instead.

This script:
1. Checks system requirements (Python, Docker, Terraform)
2. Installs Python dependencies for Vault
3. Prepares vault keys directory
4. Shows next steps for deployment

.EXAMPLE
.\install.ps1
#>

$ErrorActionPreference = "Stop"
$VERSION = "0.2.0"

# Colors
function Write-Info ($Message) { Write-Host "✓ $Message" -ForegroundColor Green }
function Write-Warn ($Message) { Write-Host "⚠ $Message" -ForegroundColor Yellow }
function Write-ErrorMsg ($Message) { Write-Host "✗ $Message" -ForegroundColor Red }
function Write-Header ($Message) {
    Write-Host ""
    Write-Host "================================================" -ForegroundColor Blue
    Write-Host $Message -ForegroundColor Blue
    Write-Host "================================================" -ForegroundColor Blue
    Write-Host ""
}
function Write-Step ($Message) {
    Write-Host ""
    Write-Host "▶ $Message" -ForegroundColor Blue
}

# Welcome
Write-Header "Rune-Vault Setup v$VERSION"

Write-Host @"
Note: This script is for team administrators who will deploy Rune-Vault.

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

try {
    $pythonCmd = Get-Command python -ErrorAction SilentlyContinue
    if ($pythonCmd) {
        $pythonVersionOutput = & python --version 2>&1 | Out-String
        if ($pythonVersionOutput -match 'Python (\d+\.\d+)') {
            $pythonVersion = [version]$matches[1]
            if ($pythonVersion -ge [version]"3.10") {
                Write-Info "Python $($pythonVersion) found"
                $pythonFound = $true
            } else {
                Write-Warn "Python $($pythonVersion) found, but 3.10+ required"
            }
        }
    }
} catch {
    # Try python3
    try {
        $python3Cmd = Get-Command python3 -ErrorAction SilentlyContinue
        if ($python3Cmd) {
            $pythonVersionOutput = & python3 --version 2>&1 | Out-String
            if ($pythonVersionOutput -match 'Python (\d+\.\d+)') {
                $pythonVersion = [version]$matches[1]
                if ($pythonVersion -ge [version]"3.10") {
                    Write-Info "Python3 $($pythonVersion) found"
                    $pythonFound = $true
                    # Use python3 command
                    Set-Alias python python3 -Scope Script
                } else {
                    Write-Warn "Python3 $($pythonVersion) found, but 3.10+ required"
                }
            }
        }
    } catch {}
}

if (-not $pythonFound) {
    Write-ErrorMsg "Python 3.10+ not found. Please install Python from https://python.org"
    Write-Host ""
    Write-Host "After installing Python, restart PowerShell and run this script again."
    exit 1
}

# Check Docker
try {
    $dockerCmd = Get-Command docker -ErrorAction SilentlyContinue
    if ($dockerCmd) {
        $dockerVersion = & docker --version 2>&1 | Out-String
        Write-Info "Docker found: $($dockerVersion.Trim())"
    } else {
        Write-Warn "Docker not found"
        Write-Host ""
        Write-Host "Docker is required for running Rune-Vault. Install from https://docker.com"
        Write-Host "If you plan to deploy to cloud instead, you can skip Docker for now."
        Write-Host ""
    }
} catch {
    Write-Warn "Docker not found or not running"
}

# Check Terraform (optional)
try {
    $tfCmd = Get-Command terraform -ErrorAction SilentlyContinue
    if ($tfCmd) {
        $tfVersion = & terraform --version 2>&1 | Select-Object -First 1
        Write-Info "Terraform found: $($tfVersion.Trim())"
    } else {
        Write-Warn "Terraform not found (optional for cloud deployment)"
        Write-Host ""
        Write-Host "If you plan to deploy to OCI/AWS/GCP, install Terraform from https://terraform.io"
        Write-Host ""
    }
} catch {
    Write-Warn "Terraform not found (optional)"
}

# Step 2: Install Python Dependencies
Write-Step "Step 2: Installing Python Dependencies"

$VAULT_DIR = Join-Path $PSScriptRoot "mcp\vault"
$VENV_DIR = Join-Path $VAULT_DIR ".venv"
$REQUIREMENTS_FILE = Join-Path $VAULT_DIR "requirements.txt"

# Create virtual environment
Write-Host "Creating Python virtual environment..."
if (Test-Path $VENV_DIR) {
    Write-Info "Virtual environment already exists at $VENV_DIR"
} else {
    & python -m venv $VENV_DIR
    if ($LASTEXITCODE -ne 0) {
        Write-ErrorMsg "Failed to create virtual environment"
        exit 1
    }
    Write-Info "Virtual environment created at $VENV_DIR"
}

# Activate venv and install dependencies
$ACTIVATE_SCRIPT = Join-Path $VENV_DIR "Scripts\Activate.ps1"

if (Test-Path $ACTIVATE_SCRIPT) {
    Write-Host "Installing dependencies..."
    
    if (Test-Path $REQUIREMENTS_FILE) {
        # Install from requirements.txt
        & $VENV_DIR\Scripts\pip.exe install --quiet -r $REQUIREMENTS_FILE
        if ($LASTEXITCODE -ne 0) {
            Write-Warn "Some packages failed to install from requirements.txt"
            Write-Host "Attempting to install core packages..."
            & $VENV_DIR\Scripts\pip.exe install --quiet pyenvector fastmcp psutil prometheus-client
        }
    } else {
        # Install core packages directly
        & $VENV_DIR\Scripts\pip.exe install --quiet pyenvector fastmcp psutil prometheus-client
    }
    
    if ($LASTEXITCODE -eq 0) {
        Write-Info "Python dependencies installed successfully"
    } else {
        Write-ErrorMsg "Failed to install dependencies"
        exit 1
    }
} else {
    Write-ErrorMsg "Could not find activation script at $ACTIVATE_SCRIPT"
    exit 1
}

# Step 3: Prepare Vault Keys
Write-Step "Step 3: Preparing Vault Keys Directory"

$KEYS_DIR = Join-Path $PSScriptRoot "vault_keys"

if (-not (Test-Path $KEYS_DIR)) {
    New-Item -ItemType Directory -Force -Path $KEYS_DIR | Out-Null
    Write-Info "Created vault_keys directory"
} else {
    Write-Info "vault_keys directory already exists"
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
