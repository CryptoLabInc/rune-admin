# Rune-Vault Test Suite

Unit and integration tests for Rune-Vault MCP server.

## Structure

```
tests/
├── unit/                      # Unit tests (isolated components)
│   ├── test_auth.py          # Token validation
│   ├── test_crypto.py        # Key generation, encryption/decryption
│   ├── test_public_key.py    # get_public_key tool
│   └── test_decrypt_scores.py # decrypt_scores tool, Top-K
├── integration/              # Integration tests (full API)
│   └── test_vault_api.py     # MCP API endpoints, E2E flows
└── load/                      # Load/performance tests
    └── load_test.py          # Locust load tests
```

## Running Tests

### Prerequisites

```bash
# Install test dependencies
pip install -r tests/requirements.txt

# Or if running from mcp/vault:
pip install -r requirements.txt pytest pytest-asyncio pytest-cov httpx
```

### Run All Tests

⚠️ **Note**: Crypto tests generate FHE keys which require significant memory (~4GB per keyset). Run tests sequentially on systems with limited RAM.

```bash
# From repository root
pytest tests/unit tests/integration -v

# Sequential execution (for limited RAM)
pytest tests/unit -v -x

# Or from tests/ directory
cd tests
pytest unit integration -v
```

### Run Specific Test Suites

```bash
# Unit tests only
pytest tests/unit -v

# Integration tests only
pytest tests/integration -v

# Specific test file
pytest tests/unit/test_auth.py -v

# Specific test function
pytest tests/unit/test_auth.py::TestTokenValidation::test_valid_token_team_alpha -v
```

### Run with Coverage

```bash
# Install coverage
pip install pytest-cov

# Run with coverage report
pytest tests/unit tests/integration --cov=mcp/vault --cov-report=html

# View report
open htmlcov/index.html  # macOS
# or
start htmlcov/index.html  # Windows
```

## Test Categories

### Unit Tests

**Isolated component testing with mocks:**

1. **Authentication** (`test_auth.py`)
   - Valid/invalid token validation
   - Case sensitivity
   - Edge cases (empty, None, whitespace)

2. **Cryptography** (`test_crypto.py`)
   - Key generation and loading
   - Encrypt/decrypt roundtrip
   - Wrong key detection
   - Dimension mismatch handling

3. **Public Key** (`test_public_key.py`)
   - Key bundle retrieval
   - Authentication enforcement
   - JSON format validation
   - Missing key handling

4. **Decrypt Scores** (`test_decrypt_scores.py`)
   - Score decryption
   - Top-K filtering (correctness)
   - Top-K sorting (descending)
   - Rate limiting (max 10)
   - Error handling

### Integration Tests

**Full API testing with real components:**

1. **Vault API** (`test_vault_api.py`)
   - MCP endpoint availability
   - End-to-end encrypt → decrypt flow
   - Concurrent request handling
   - Cross-endpoint authentication
   - Rate limiting enforcement

## Writing Tests

### Test Fixtures

Use fixtures for common setup:

```python
@pytest.fixture(scope="class")
def crypto_keys():
    """Generate test keys once for all tests in class."""
    temp_dir = tempfile.mkdtemp()
    # ... setup code ...
    yield {"key_dir": temp_dir, ...}
    shutil.rmtree(temp_dir)
```

### Mocking

Use `monkeypatch` for isolated tests:

```python
def test_with_temp_keys(monkeypatch):
    monkeypatch.setattr('vault_mcp.KEY_DIR', '/tmp/test_keys')
    # ... test code ...
```

### Async Tests

Mark async tests with decorator:

```python
@pytest.mark.asyncio
async def test_api_endpoint():
    async with AsyncClient(app=app) as client:
        response = await client.get("/endpoint")
        assert response.status_code == 200
```

## Continuous Integration

### GitHub Actions Example

```yaml
name: Tests

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-python@v4
        with:
          python-version: '3.10'
      
      - name: Install dependencies
        run: |
          cd mcp/vault
          python -m venv .venv
          source .venv/bin/activate
          pip install -r requirements.txt
          pip install pytest pytest-asyncio pytest-cov httpx
      
      - name: Run tests
        run: |
          source mcp/vault/.venv/bin/activate
          pytest tests/unit tests/integration -v --cov=mcp/vault
      
      - name: Upload coverage
        uses: codecov/codecov-action@v3
```

## Test Coverage Goals

| Component | Target Coverage |
|-----------|----------------|
| Authentication | 100% |
| Key Management | 90%+ |
| MCP Tools | 95%+ |
| Error Handling | 90%+ |
| Overall | 90%+ |

## Troubleshooting

### Import Errors

If you see `ModuleNotFoundError: No module named 'vault_mcp'`:

```bash
# Ensure you're running from repository root
export PYTHONPATH="${PYTHONPATH}:$(pwd)/mcp/vault"
pytest tests/unit -v
```

### Key Generation Slow

Key generation takes time (FHE keys are large). Tests use small dimensions (8-32) for speed:

```python
# Use dim=8 or 16 for fast tests
keygen = KeyGenerator(key_path=temp_dir, key_id="test", dim_list=[8])
```

### Concurrent Test Failures

If tests fail when run in parallel:

```bash
# Run tests sequentially
pytest tests/unit tests/integration -v -n0
```

## Adding New Tests

1. **Choose category**: Unit vs Integration
2. **Create test file**: `test_<feature>.py`
3. **Write test class**: `TestFeatureName`
4. **Add fixtures**: Setup/teardown logic
5. **Write tests**: One test per behavior
6. **Run locally**: Verify all pass
7. **Submit PR**: Include test coverage report

## Performance Benchmarks

Unit tests should be fast:
- Auth tests: < 0.1s each
- Crypto tests: < 2s each (key generation)
- API tests: < 1s each

Integration tests can be slower:
- E2E flow: < 5s
- Concurrent tests: < 10s

Run benchmarks:
```bash
pytest tests/unit tests/integration --durations=10
```
