# Claude Integration Setup Guide

This guide provides **verified, working** configurations for integrating Rune with Claude.

## Prerequisites

1. **Install Dependencies:**
```bash
python3 -m venv .vault_venv
source .vault_venv/bin/activate
pip install mcp pyenvector uvicorn numpy
```

2. **Start Vault Server:**
```bash
./scripts/vault-dev.sh
# ✓ Server running on http://localhost:50080
```

---

## Option 1: Claude Code (VS Code Extension)

### Configuration Location
Create or edit: `.mcp/settings.json` in your workspace root

### Configuration File
```json
{
  "servers": {
    "rune-vault": {
      "type": "stdio",
      "command": "python3",
      "args": [
        "/absolute/path/to/rune/mcp/vault/vault_mcp.py"
      ],
      "cwd": "/absolute/path/to/rune/mcp/vault",
      "env": {
        "RUNEVAULT_TOKEN": "envector-team-alpha"
      }
    }
  }
}
```

**Important:**
- Replace `/absolute/path/to/rune/...` with your actual Rune installation path
- Use forward slashes even on Windows in JSON
- The `cwd` ensures keys are found in the correct directory

### Platform-Specific Paths

**macOS / Linux:**
```json
{
  "servers": {
    "rune-vault": {
      "command": "python3",
      "args": [
        "/absolute/path/to/rune/mcp/vault/vault_mcp.py"
      ],
      "cwd": "/absolute/path/to/rune/mcp/vault"
    }
  }
}
```

**Windows (use forward slashes in JSON):**
```json
{
  "servers": {
    "rune-vault": {
      "command": "python",
      "args": [
        "C:/absolute/path/to/rune/mcp/vault/vault_mcp.py"
      ],
      "cwd": "C:/absolute/path/to/rune/mcp/vault",
      "env": {
        "RUNEVAULT_TOKEN": "envector-team-alpha"
      }
    }
  }
}
```

**Windows (PowerShell - alternative):**
```json
{
  "servers": {
    "rune-vault": {
      "command": "powershell.exe",
      "args": [
        "-Command",
        "python",
        "C:/absolute/path/to/rune/mcp/vault/vault_mcp.py"
      ],
      "cwd": "C:/absolute/path/to/rune/mcp/vault",
      "env": {
        "RUNEVAULT_TOKEN": "envector-team-alpha"
      }
    }
  }
}
```

### How to Find Your Absolute Path

**macOS / Linux:**
```bash
cd /absolute/path/to/rune/mcp/vault
pwd  # Copy this output
```

**Windows (Command Prompt):**
```cmd
cd C:\path\to\rune\mcp\vault
cd  # Shows current directory
```

**Windows (PowerShell):**
```powershell
cd C:\path\to\rune\mcp\vault
Get-Location  # Shows current directory
```

**Tip**: When copying Windows paths to JSON, replace backslashes `\` with forward slashes `/`
```
C:\Users\Alice\repo\rune  →  C:/Users/Alice/repo/rune
```

### Verify Installation
1. Open VS Code with Claude extension
2. Open Command Palette (Cmd+Shift+P / Ctrl+Shift+P)
3. Search for "MCP: Show Connected Servers"
4. You should see "rune-vault" listed

### Test the Integration
Ask Claude:
```
Can you call the get_public_key tool from the vault server with token "envector-team-alpha"?
```

Expected response:
- Claude should invoke the tool
- Receive a JSON bundle with EncKey, EvalKey

---

## Option 2: Claude Desktop (macOS)

### Configuration Location
Edit: `~/Library/Application Support/Claude/config.json`

### Configuration File
```json
{
  "mcpServers": {
    "rune-vault": {
      "command": "python3",
      "args": [
        "/absolute/path/to/rune/mcp/vault/vault_mcp.py"
      ],
      "env": {
        "PYTHONPATH": "/absolute/path/to/rune/.vault_venv/lib/python3.12/site-packages",
        "RUNEVAULT_TOKEN": "envector-team-alpha"
      }
    }
  }
}
```

**Important:**
- Replace paths with your actual locations
- Update Python version in PYTHONPATH (check with `python3 --version`)
- On Windows: Use `%APPDATA%\Claude\config.json`
- On Linux: Use `~/.config/claude/config.json`

### Verify Installation
1. Restart Claude Desktop
2. Open a new conversation
3. Ask: "What tools do you have access to?"
4. Should list: `get_public_key`, `decrypt_scores`

---

## Option 3: Claude Desktop (Windows)

### Configuration Location
Edit: `%APPDATA%\Claude\config.json`

### Configuration File
```json
{
  "mcpServers": {
    "rune-vault": {
      "command": "python",
      "args": [
        "C:\\Users\\YOUR_USERNAME\\repo\\cryptolab\\rune\\mcp\\vault\\vault_mcp.py"
      ],
      "env": {
        "PYTHONPATH": "C:\\Users\\YOUR_USERNAME\\repo\\cryptolab\\rune\\.vault_venv\\Lib\\site-packages",
        "RUNEVAULT_TOKEN": "envector-team-alpha"
      }
    }
  }
}
```

**Important:**
- Use double backslashes `\\` in paths
- Or use forward slashes `/` (JSON accepts both on Windows)
- Adjust Python path to your installation

---

## Troubleshooting

### Error: "Module 'mcp' not found"

**Problem:** Python can't find the installed packages

**Solution:** Add PYTHONPATH to env:
```json
"env": {
  "PYTHONPATH": "/absolute/path/to/.vault_venv/lib/python3.12/site-packages",
  "RUNEVAULT_TOKEN": "envector-team-alpha"
}
```

### Error: "Command not found: python3"

**Problem:** Python command name differs on your system

**Solution:** Try these alternatives:
- `python` instead of `python3`
- `/usr/bin/python3` (absolute path)
- Check with: `which python3`

### Error: "Keys not found"

**Problem:** vault_mcp.py can't find vault_keys directory

**Solution:** Add `cwd` to point to vault directory:
```json
"cwd": "/absolute/path/to/rune/mcp/vault"
```

### Error: "Access Denied: Invalid Token"

**Problem:** Wrong token in configuration

**Solution:** Use one of these valid tokens:
- `envector-team-alpha`
- `envector-admin-001`

### MCP Server Not Connecting

**Checklist:**
1. ✓ Vault server is running: `./scripts/vault-dev.sh`
2. ✓ Absolute paths in config (not relative)
3. ✓ Python environment has mcp, pyenvector installed
4. ✓ Config file is valid JSON (check with jsonlint)
5. ✓ Restart Claude after config changes

---

## Testing Your Setup

### Test 1: List Available Tools
Ask Claude:
```
What MCP tools do you have from the rune-vault server?
```

Expected:
- `get_public_key(token: str) -> str`
- `decrypt_scores(token: str, encrypted_blob_b64: str, top_k: int) -> str`

### Test 2: Call get_public_key
Ask Claude:
```
Call the get_public_key tool with token "envector-team-alpha"
```

Expected:
- JSON response with two keys
- EncKey.json, EvalKey.json

### Test 3: Verify Error Handling
Ask Claude:
```
Call get_public_key with an invalid token "wrong-token"
```

Expected:
- Error message: "Access Denied: Invalid Authentication Token"

---

## Advanced Configuration

### Running Vault on Different Port
If port 50080 is taken:

1. **Start vault on custom port:**
```bash
cd mcp/vault
source ../../.vault_venv/bin/activate
python3 vault_mcp.py server --port 8080
```

2. **Update Claude config:**
```json
"env": {
  "RUNEVAULT_ENDPOINT": "http://localhost:8080",
  "RUNEVAULT_TOKEN": "envector-team-alpha"
}
```

### Using Remote Vault
For team deployment:

```json
"env": {
  "RUNEVAULT_ENDPOINT": "https://vault-your-team.oci.envector.io",
  "RUNEVAULT_TOKEN": "evt_your_production_token"
}
```

### Debug Mode
To see detailed logs:

```json
"env": {
  "RUNEVAULT_TOKEN": "envector-team-alpha",
  "MCP_DEBUG": "1",
  "PYTHONUNBUFFERED": "1"
}
```

---

## Next Steps

After successful integration:

1. **Read the Skill Documentation:**
   - See [skills/envector/SKILL.md](../skills/envector/SKILL.md)
   - Learn about organizational memory use cases

2. **Set Up Team Vault:**
   - Deploy shared vault for your team
   - See [deployment/README.md](../deployment/README.md)

---

## Support

**Issues:** https://github.com/CryptoLabInc/rune-admin/issues  
**Documentation:** [docs/AGENT-INTEGRATION.md](../docs/AGENT-INTEGRATION.md)  
**Demo:** Run `python3 mcp/vault/demo_local.py`

---

**Last Updated:** January 31, 2026  
**Tested With:**
- Claude Desktop (macOS)
- Claude Code in VS Code
- Python 3.12
- FastMCP 1.26.0
