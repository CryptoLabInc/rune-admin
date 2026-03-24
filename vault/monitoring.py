"""
Rune-Vault Monitoring Module

Provides health checks, metrics, and alerting for the Vault service.
Integrates with Prometheus for metrics collection and Grafana for visualization.
Uses stdlib http.server — no starlette/uvicorn dependency.
"""

import json
import time
import psutil
import logging
import threading
from typing import Dict, Any
from datetime import datetime
from http.server import BaseHTTPRequestHandler, HTTPServer
from prometheus_client import Counter, Gauge, Histogram, generate_latest, CONTENT_TYPE_LATEST

# Configure logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

# Prometheus metrics
vault_requests_total = Counter(
    'vault_requests_total',
    'Total number of requests',
    ['method', 'endpoint', 'status', 'user']
)

vault_request_duration = Histogram(
    'vault_request_duration_seconds',
    'Request duration in seconds',
    ['method', 'endpoint']
)

vault_decryption_operations = Counter(
    'vault_decryption_operations_total',
    'Total number of decryption operations',
    ['status']
)

vault_decryption_duration = Histogram(
    'vault_decryption_duration_seconds',
    'Decryption duration in seconds'
)

vault_key_access = Counter(
    'vault_key_access_total',
    'Total number of key access operations',
    ['key_type', 'status']
)

vault_health_status = Gauge(
    'vault_health_status',
    'Vault health status (1=healthy, 0=unhealthy)'
)

vault_cpu_usage = Gauge(
    'vault_cpu_usage_percent',
    'CPU usage percentage'
)

vault_memory_usage = Gauge(
    'vault_memory_usage_bytes',
    'Memory usage in bytes'
)

vault_uptime_seconds = Gauge(
    'vault_uptime_seconds',
    'Vault uptime in seconds'
)

# Service state
service_start_time = time.time()
last_health_check = None
health_status = "unknown"
_state_lock = threading.Lock()

class HealthChecker:
    """
    Health check manager for Rune-Vault.
    Performs various checks to determine service health.
    """

    def __init__(self):
        self.checks = {
            "keys": self._check_keys,
            "memory": self._check_memory,
            "cpu": self._check_cpu,
            "disk": self._check_disk,
        }

    def _check_keys(self) -> Dict[str, Any]:
        """Check if FHE keys are accessible"""
        import os
        try:
            key_dir = os.getenv("KEY_DIR", "vault_keys")
            key_id = os.getenv("KEY_ID", "vault-key")
            key_subdir = os.path.join(key_dir, key_id)
            required_keys = ["EncKey.json", "SecKey.json", "EvalKey.json"]

            missing_keys = []
            for key_file in required_keys:
                key_path = os.path.join(key_subdir, key_file)
                if not os.path.exists(key_path):
                    missing_keys.append(key_file)

            if missing_keys:
                return {
                    "status": "unhealthy",
                    "message": f"Missing keys: {', '.join(missing_keys)}"
                }

            return {
                "status": "healthy",
                "message": "All keys present"
            }
        except Exception as e:
            return {
                "status": "unhealthy",
                "message": f"Key check failed: {str(e)}"
            }

    def _check_memory(self) -> Dict[str, Any]:
        """Check memory usage"""
        try:
            memory = psutil.virtual_memory()
            vault_memory_usage.set(memory.used)

            if memory.percent > 90:
                return {
                    "status": "unhealthy",
                    "message": f"High memory usage: {memory.percent}%"
                }
            elif memory.percent > 80:
                return {
                    "status": "degraded",
                    "message": f"Elevated memory usage: {memory.percent}%"
                }

            return {
                "status": "healthy",
                "message": f"Memory usage: {memory.percent}%"
            }
        except Exception as e:
            return {
                "status": "unknown",
                "message": f"Memory check failed: {str(e)}"
            }

    def _check_cpu(self) -> Dict[str, Any]:
        """Check CPU usage"""
        try:
            cpu_percent = psutil.cpu_percent(interval=1)
            vault_cpu_usage.set(cpu_percent)

            if cpu_percent > 90:
                return {
                    "status": "unhealthy",
                    "message": f"High CPU usage: {cpu_percent}%"
                }
            elif cpu_percent > 80:
                return {
                    "status": "degraded",
                    "message": f"Elevated CPU usage: {cpu_percent}%"
                }

            return {
                "status": "healthy",
                "message": f"CPU usage: {cpu_percent}%"
            }
        except Exception as e:
            return {
                "status": "unknown",
                "message": f"CPU check failed: {str(e)}"
            }

    def _check_disk(self) -> Dict[str, Any]:
        """Check disk usage"""
        try:
            disk = psutil.disk_usage('/')

            if disk.percent > 90:
                return {
                    "status": "unhealthy",
                    "message": f"Critical disk usage: {disk.percent}%"
                }
            elif disk.percent > 80:
                return {
                    "status": "degraded",
                    "message": f"High disk usage: {disk.percent}%"
                }

            return {
                "status": "healthy",
                "message": f"Disk usage: {disk.percent}%"
            }
        except Exception as e:
            return {
                "status": "unknown",
                "message": f"Disk check failed: {str(e)}"
            }

    def run_checks(self) -> Dict[str, Any]:
        """Run all health checks (synchronous)."""
        global last_health_check, health_status

        results = {}
        overall_status = "healthy"

        for check_name, check_func in self.checks.items():
            try:
                result = check_func()
                results[check_name] = result

                # Determine overall status
                if result["status"] == "unhealthy":
                    overall_status = "unhealthy"
                elif result["status"] == "degraded" and overall_status == "healthy":
                    overall_status = "degraded"
                elif result["status"] == "unknown" and overall_status in ["healthy", "degraded"]:
                    overall_status = "degraded"

            except Exception as e:
                logger.error(f"Health check '{check_name}' failed: {e}")
                results[check_name] = {
                    "status": "unknown",
                    "message": f"Check failed: {str(e)}"
                }
                overall_status = "degraded"

        # Update metrics
        vault_health_status.set(1 if overall_status == "healthy" else 0)
        vault_uptime_seconds.set(time.time() - service_start_time)

        with _state_lock:
            last_health_check = datetime.now().isoformat()
            health_status = overall_status

        return {
            "status": overall_status,
            "timestamp": last_health_check,
            "uptime_seconds": time.time() - service_start_time,
            "checks": results
        }

# Initialize health checker
health_checker = HealthChecker()


class MonitoringHandler(BaseHTTPRequestHandler):
    """HTTP request handler for monitoring endpoints using stdlib."""

    def log_message(self, format, *args):
        # Suppress default stderr logging; use our logger instead
        logger.debug(format, *args)

    def _send_json(self, data: dict, status_code: int = 200):
        body = json.dumps(data).encode("utf-8")
        self.send_response(status_code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == "/health":
            result = health_checker.run_checks()
            status_code = 200 if result["status"] in ["healthy", "degraded"] else 503
            self._send_json(result, status_code)

        elif self.path == "/health/ready":
            result = health_checker.run_checks()
            if result["checks"].get("keys", {}).get("status") != "healthy":
                self._send_json({"status": "not ready", "reason": "keys not accessible"}, 503)
            else:
                self._send_json({"status": "ready"}, 200)

        elif self.path == "/health/live":
            self._send_json({"status": "alive"}, 200)

        elif self.path == "/metrics":
            body = generate_latest()
            self.send_response(200)
            self.send_header("Content-Type", CONTENT_TYPE_LATEST)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        elif self.path == "/status":
            try:
                cpu_pct = psutil.cpu_percent(interval=0.1)
                mem_pct = psutil.virtual_memory().percent
                disk_pct = psutil.disk_usage('/').percent
            except Exception:
                cpu_pct = None
                mem_pct = None
                disk_pct = None
            with _state_lock:
                current_status = health_status
                current_check = last_health_check
            self._send_json({
                "service": "Rune-Vault",
                "version": "0.4.0",
                "status": current_status,
                "uptime_seconds": time.time() - service_start_time,
                "last_health_check": current_check,
                "metrics": {
                    "cpu_percent": cpu_pct,
                    "memory_percent": mem_pct,
                    "disk_percent": disk_pct,
                }
            })

        else:
            self.send_response(404)
            self.end_headers()


def _periodic_health_check(interval: int = 60):
    """Run health checks periodically in a daemon thread."""
    while True:
        try:
            health_checker.run_checks()
            logger.info(f"Periodic health check completed: {health_status}")
        except Exception as e:
            logger.error(f"Periodic health check failed: {e}")
        time.sleep(interval)


def start_monitoring(port: int = 9090):
    """
    Start the monitoring HTTP server and periodic health check in daemon threads.
    Non-blocking — returns immediately.
    """
    # HTTP server thread
    server = HTTPServer(("0.0.0.0", port), MonitoringHandler)
    http_thread = threading.Thread(target=server.serve_forever, daemon=True)
    http_thread.start()

    # Periodic health check thread
    hc_thread = threading.Thread(target=_periodic_health_check, daemon=True)
    hc_thread.start()

    logger.info(f"Monitoring endpoints available on :{port} (/health, /metrics, /status)")


# Alert conditions
class AlertManager:
    """
    Simple alert manager for Rune-Vault.
    Logs alerts that can be picked up by external monitoring systems.
    """

    @staticmethod
    def check_and_alert():
        """Check conditions and generate alerts"""
        alerts = []

        # Memory alert
        memory = psutil.virtual_memory()
        if memory.percent > 90:
            alerts.append({
                "severity": "critical",
                "alert": "HighMemoryUsage",
                "message": f"Memory usage is {memory.percent}%",
                "value": memory.percent
            })

        # CPU alert
        cpu_percent = psutil.cpu_percent(interval=0.1)
        if cpu_percent > 90:
            alerts.append({
                "severity": "critical",
                "alert": "HighCPUUsage",
                "message": f"CPU usage is {cpu_percent}%",
                "value": cpu_percent
            })

        # Disk alert
        disk = psutil.disk_usage('/')
        if disk.percent > 90:
            alerts.append({
                "severity": "critical",
                "alert": "HighDiskUsage",
                "message": f"Disk usage is {disk.percent}%",
                "value": disk.percent
            })

        # Health status alert
        if health_status == "unhealthy":
            alerts.append({
                "severity": "critical",
                "alert": "VaultUnhealthy",
                "message": "Vault health check failed",
                "last_check": last_health_check
            })

        # Log alerts
        for alert in alerts:
            logger.warning(f"ALERT: {alert}")

        return alerts
