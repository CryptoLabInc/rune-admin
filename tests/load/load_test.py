"""
Rune-Vault Load Testing Script

Uses Locust to simulate concurrent Vault decryption requests and measure performance.
Tests throughput, latency distribution, and identifies bottlenecks.

Usage:
    # Install dependencies
    pip install locust

    # Run test (Web UI)
    locust -f load_test.py --host=https://vault-yourteam.oci.envector.io

    # Run headless test
    locust -f load_test.py --host=https://vault-yourteam.oci.envector.io \
           --users=50 --spawn-rate=5 --run-time=5m --headless

    # Generate report
    locust -f load_test.py --host=https://vault-yourteam.oci.envector.io \
           --users=100 --spawn-rate=10 --run-time=10m --headless \
           --html=report.html --csv=results
"""

import time
import os
import json
import random
from locust import HttpUser, task, between, events
import base64
import numpy as np

# Configuration
RUNEVAULT_TOKEN = os.getenv("RUNEVAULT_TOKEN", "evt_test_token")
TEST_DIMENSION = 1024

# Sample encrypted data (mocked for testing)
def generate_mock_encrypted_vector():
    """Generate a mock encrypted vector for testing"""
    # In real scenario, this would be a real encrypted vector from envector-mcp-server
    mock_vector = np.random.rand(TEST_DIMENSION).astype(np.float32)
    return base64.b64encode(mock_vector.tobytes()).decode('utf-8')

def generate_mock_encrypted_query():
    """Generate a mock encrypted query for testing"""
    mock_query = np.random.rand(TEST_DIMENSION).astype(np.float32)
    return base64.b64encode(mock_query.tobytes()).decode('utf-8')

class VaultUser(HttpUser):
    """
    Simulates a user making requests to Rune-Vault.
    Each user represents an agent instance making decrypt requests.
    """
    
    wait_time = between(1, 3)  # Wait 1-3 seconds between tasks
    
    def on_start(self):
        """Called when a user starts. Setup user context."""
        self.headers = {
            "Content-Type": "application/json",
            "Authorization": f"Bearer {RUNEVAULT_TOKEN}"
        }
        
        # Pre-generate some test data
        self.encrypted_vectors = [generate_mock_encrypted_vector() for _ in range(10)]
        self.encrypted_queries = [generate_mock_encrypted_query() for _ in range(5)]
    
    @task(3)
    def health_check(self):
        """Check Vault health (lightweight request)"""
        with self.client.get(
            "/health",
            catch_response=True,
            name="/health"
        ) as response:
            if response.status_code == 200:
                response.success()
            else:
                response.failure(f"Health check failed: {response.status_code}")
    
    @task(5)
    def get_public_key(self):
        """Get public key bundle (common operation)"""
        payload = {
            "token": RUNEVAULT_TOKEN
        }
        
        start_time = time.time()
        with self.client.post(
            "/tools/get_public_key",
            json=payload,
            headers=self.headers,
            catch_response=True,
            name="/tools/get_public_key"
        ) as response:
            elapsed_time = time.time() - start_time
            
            if response.status_code == 200:
                response.success()
                # Track latency
                events.request.fire(
                    request_type="custom",
                    name="get_public_key_latency",
                    response_time=elapsed_time * 1000,
                    response_length=len(response.content),
                    exception=None,
                    context={}
                )
            else:
                response.failure(f"Get public key failed: {response.status_code}")
    
    @task(10)
    def decrypt_search_result(self):
        """Decrypt search result (core operation, most frequent)"""
        payload = {
            "token": RUNEVAULT_TOKEN,
            "encrypted_result": random.choice(self.encrypted_vectors),
            "metadata": {
                "query_id": f"query_{random.randint(1000, 9999)}",
                "timestamp": time.time()
            }
        }
        
        start_time = time.time()
        with self.client.post(
            "/tools/decrypt_search_result",
            json=payload,
            headers=self.headers,
            catch_response=True,
            name="/tools/decrypt_search_result"
        ) as response:
            elapsed_time = time.time() - start_time
            
            if response.status_code == 200:
                response.success()
                # Track decryption latency (critical metric)
                events.request.fire(
                    request_type="custom",
                    name="decrypt_latency",
                    response_time=elapsed_time * 1000,
                    response_length=len(response.content),
                    exception=None,
                    context={}
                )
            else:
                response.failure(f"Decrypt failed: {response.status_code}")
    
    @task(1)
    def metrics_endpoint(self):
        """Check metrics endpoint (monitoring)"""
        with self.client.get(
            "/metrics",
            catch_response=True,
            name="/metrics"
        ) as response:
            if response.status_code == 200:
                response.success()
            else:
                response.failure(f"Metrics failed: {response.status_code}")

class BurstVaultUser(HttpUser):
    """
    Simulates burst traffic patterns (sudden spikes in requests).
    Useful for testing how Vault handles load spikes.
    """
    
    wait_time = between(0.1, 0.5)  # Very short wait time = high burst
    
    def on_start(self):
        """Called when a user starts."""
        self.headers = {
            "Content-Type": "application/json",
            "Authorization": f"Bearer {RUNEVAULT_TOKEN}"
        }
        self.encrypted_vectors = [generate_mock_encrypted_vector() for _ in range(10)]
    
    @task
    def rapid_decrypt(self):
        """Rapid-fire decrypt requests"""
        payload = {
            "token": RUNEVAULT_TOKEN,
            "encrypted_result": random.choice(self.encrypted_vectors)
        }
        
        with self.client.post(
            "/tools/decrypt_search_result",
            json=payload,
            headers=self.headers,
            catch_response=True,
            name="/tools/decrypt_search_result [burst]"
        ) as response:
            if response.status_code != 200:
                response.failure(f"Burst decrypt failed: {response.status_code}")

# Custom event listeners for detailed metrics
@events.test_start.add_listener
def on_test_start(environment, **kwargs):
    """Called when test starts"""
    print("\n" + "="*60)
    print("Rune-Vault Load Test Started")
    print(f"Target: {environment.host}")
    print(f"Token: {RUNEVAULT_TOKEN[:20]}...")
    print("="*60 + "\n")

@events.test_stop.add_listener
def on_test_stop(environment, **kwargs):
    """Called when test stops. Print summary."""
    print("\n" + "="*60)
    print("Rune-Vault Load Test Completed")
    print("="*60)
    
    # Get stats
    stats = environment.stats
    
    print("\n📊 Summary Statistics:")
    print(f"  Total Requests: {stats.total.num_requests}")
    print(f"  Total Failures: {stats.total.num_failures}")
    print(f"  Median Response Time: {stats.total.median_response_time} ms")
    print(f"  Average Response Time: {stats.total.avg_response_time:.2f} ms")
    print(f"  Min Response Time: {stats.total.min_response_time} ms")
    print(f"  Max Response Time: {stats.total.max_response_time} ms")
    print(f"  Requests/sec: {stats.total.total_rps:.2f}")
    print(f"  Failures/sec: {stats.total.fail_ratio:.2%}")
    
    print("\n🔍 Per-Endpoint Breakdown:")
    for name, entry in stats.entries.items():
        if entry.num_requests > 0:
            print(f"\n  {name}:")
            print(f"    Requests: {entry.num_requests}")
            print(f"    Failures: {entry.num_failures} ({entry.fail_ratio:.2%})")
            print(f"    Median: {entry.median_response_time} ms")
            print(f"    Average: {entry.avg_response_time:.2f} ms")
            print(f"    95th percentile: {entry.get_response_time_percentile(0.95)} ms")
            print(f"    99th percentile: {entry.get_response_time_percentile(0.99)} ms")
    
    print("\n" + "="*60 + "\n")

# Test scenarios
class QuickTest(HttpUser):
    """
    Quick smoke test (10 users, 1 minute).
    Use for quick validation before full load test.
    """
    tasks = [VaultUser]
    wait_time = between(1, 2)

class SustainedLoadTest(HttpUser):
    """
    Sustained load test (50 users, 10 minutes).
    Use for baseline performance measurement.
    """
    tasks = [VaultUser]
    wait_time = between(1, 3)

class StressTest(HttpUser):
    """
    Stress test (100+ users, until failure).
    Use to find breaking point.
    """
    tasks = [VaultUser, BurstVaultUser]
    wait_time = between(0.5, 2)
