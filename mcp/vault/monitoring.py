"""
Rune-Vault Monitoring Module

Provides health checks, metrics, and alerting for the Vault service.
Integrates with Prometheus for metrics collection and Grafana for visualization.
"""

import time
import psutil
import logging
from typing import Dict, Any
from datetime import datetime
from starlette.applications import Starlette
from starlette.responses import JSONResponse, Response
from starlette.routing import Route
from prometheus_client import Counter, Gauge, Histogram, generate_latest, CONTENT_TYPE_LATEST
import asyncio

# Configure logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

# Prometheus metrics
vault_requests_total = Counter(
    'vault_requests_total',
    'Total number of requests',
    ['method', 'endpoint', 'status']
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
            required_keys = ["EncKey.json", "SecKey.json", "EvalKey.json", "MetadataKey.json"]
            
            missing_keys = []
            for key_file in required_keys:
                key_path = os.path.join(key_dir, key_file)
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
    
    async def run_checks(self) -> Dict[str, Any]:
        """Run all health checks"""
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

# FastAPI endpoints for health and metrics
def add_monitoring_endpoints(app):
    """
    Add monitoring endpoints to the FastAPI/Starlette app.
    Call this from your main vault_mcp.py.
    """
    
    async def health(request):
        """
        Health check endpoint.
        Returns 200 if healthy, 503 if unhealthy.
        """
        result = await health_checker.run_checks()
        status_code = 200 if result["status"] in ["healthy", "degraded"] else 503
        return JSONResponse(content=result, status_code=status_code)
    
    async def readiness(request):
        """
        Readiness check (for Kubernetes).
        Returns 200 if service is ready to accept traffic.
        """
        result = await health_checker.run_checks()
        
        # Check if keys are accessible
        if result["checks"].get("keys", {}).get("status") != "healthy":
            return JSONResponse(
                content={"status": "not ready", "reason": "keys not accessible"},
                status_code=503
            )
        
        return JSONResponse(content={"status": "ready"}, status_code=200)
    
    async def liveness(request):
        """
        Liveness check (for Kubernetes).
        Returns 200 if service is alive (not deadlocked).
        """
        return JSONResponse(content={"status": "alive"}, status_code=200)
    
    async def metrics_endpoint(request):
        """
        Prometheus metrics endpoint.
        """
        return Response(
            content=generate_latest(),
            media_type=CONTENT_TYPE_LATEST
        )
    
    async def status(request):
        """
        Detailed status information.
        """
        return JSONResponse(content={
            "service": "Rune-Vault",
            "version": "0.1.0",
            "status": health_status,
            "uptime_seconds": time.time() - service_start_time,
            "last_health_check": last_health_check,
            "metrics": {
                "cpu_percent": psutil.cpu_percent(interval=0.1),
                "memory_percent": psutil.virtual_memory().percent,
                "disk_percent": psutil.disk_usage('/').percent,
            }
        })

    # Use add_route for compatibility with both FastAPI and Starlette
    app.add_route("/health", health, methods=["GET"])
    app.add_route("/health/ready", readiness, methods=["GET"])
    app.add_route("/health/live", liveness, methods=["GET"])
    app.add_route("/metrics", metrics_endpoint, methods=["GET"])
    app.add_route("/status", status, methods=["GET"])

# Background task for periodic health checks
async def periodic_health_check(interval: int = 60):
    """
    Run health checks periodically in the background.
    """
    while True:
        try:
            await health_checker.run_checks()
            logger.info(f"Periodic health check completed: {health_status}")
        except Exception as e:
            logger.error(f"Periodic health check failed: {e}")
        
        await asyncio.sleep(interval)

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
