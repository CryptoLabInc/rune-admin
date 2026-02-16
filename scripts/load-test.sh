#!/bin/bash

# Rune-Vault Load Testing Runner
# Provides pre-configured test scenarios for common load testing needs

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
RUNEVAULT_ENDPOINT="${RUNEVAULT_ENDPOINT:-https://vault-demo.oci.envector.io}"
RUNEVAULT_TOKEN="${RUNEVAULT_TOKEN:-}"
OUTPUT_DIR="${OUTPUT_DIR:-./load-test-results}"

# Ensure output directory exists
mkdir -p "$OUTPUT_DIR"

print_header() {
    echo -e "\n${BLUE}================================================${NC}"
    echo -e "${BLUE}$1${NC}"
    echo -e "${BLUE}================================================${NC}\n"
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}⚠ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

check_requirements() {
    print_header "Checking Requirements"
    
    # Check if locust is installed
    if ! command -v locust &> /dev/null; then
        print_error "Locust is not installed"
        echo "Install with: pip install locust"
        exit 1
    fi
    print_success "Locust is installed"
    
    # Check if Vault URL is set
    if [ -z "$RUNEVAULT_ENDPOINT" ]; then
        print_error "RUNEVAULT_ENDPOINT is not set"
        echo "Set with: export RUNEVAULT_ENDPOINT=https://vault-yourteam.oci.envector.io"
        exit 1
    fi
    print_success "Vault URL: $RUNEVAULT_ENDPOINT"
    
    # Check if Vault token is set
    if [ -z "$RUNEVAULT_TOKEN" ]; then
        print_warning "RUNEVAULT_TOKEN is not set (some tests may fail)"
    else
        print_success "Vault token is set"
    fi
    
    # Check if Vault is reachable
    if curl -s --max-time 5 "$RUNEVAULT_ENDPOINT/health" > /dev/null 2>&1; then
        print_success "Vault is reachable"
    else
        print_error "Vault is not reachable at $RUNEVAULT_ENDPOINT"
        exit 1
    fi
}

run_smoke_test() {
    print_header "Running Smoke Test (Quick Validation)"
    
    echo "Configuration:"
    echo "  Users: 5"
    echo "  Spawn Rate: 1 user/sec"
    echo "  Duration: 1 minute"
    echo ""
    
    locust -f load_test.py \
        --host="$RUNEVAULT_ENDPOINT" \
        --users=5 \
        --spawn-rate=1 \
        --run-time=1m \
        --headless \
        --html="$OUTPUT_DIR/smoke-test-$(date +%Y%m%d-%H%M%S).html" \
        --csv="$OUTPUT_DIR/smoke-test-$(date +%Y%m%d-%H%M%S)"
    
    print_success "Smoke test completed"
}

run_baseline_test() {
    print_header "Running Baseline Performance Test"
    
    echo "Configuration:"
    echo "  Users: 25"
    echo "  Spawn Rate: 5 users/sec"
    echo "  Duration: 5 minutes"
    echo ""
    
    locust -f load_test.py \
        --host="$RUNEVAULT_ENDPOINT" \
        --users=25 \
        --spawn-rate=5 \
        --run-time=5m \
        --headless \
        --html="$OUTPUT_DIR/baseline-test-$(date +%Y%m%d-%H%M%S).html" \
        --csv="$OUTPUT_DIR/baseline-test-$(date +%Y%m%d-%H%M%S)"
    
    print_success "Baseline test completed"
}

run_sustained_load_test() {
    print_header "Running Sustained Load Test"
    
    echo "Configuration:"
    echo "  Users: 50"
    echo "  Spawn Rate: 5 users/sec"
    echo "  Duration: 10 minutes"
    echo ""
    
    locust -f load_test.py \
        --host="$RUNEVAULT_ENDPOINT" \
        --users=50 \
        --spawn-rate=5 \
        --run-time=10m \
        --headless \
        --html="$OUTPUT_DIR/sustained-test-$(date +%Y%m%d-%H%M%S).html" \
        --csv="$OUTPUT_DIR/sustained-test-$(date +%Y%m%d-%H%M%S)"
    
    print_success "Sustained load test completed"
}

run_stress_test() {
    print_header "Running Stress Test (Find Breaking Point)"
    
    echo "Configuration:"
    echo "  Users: 100"
    echo "  Spawn Rate: 10 users/sec"
    echo "  Duration: 15 minutes"
    echo ""
    
    print_warning "This test may cause Vault to become overloaded"
    read -p "Continue? (y/N) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "Stress test cancelled"
        return
    fi
    
    locust -f load_test.py \
        --host="$RUNEVAULT_ENDPOINT" \
        --users=100 \
        --spawn-rate=10 \
        --run-time=15m \
        --headless \
        --html="$OUTPUT_DIR/stress-test-$(date +%Y%m%d-%H%M%S).html" \
        --csv="$OUTPUT_DIR/stress-test-$(date +%Y%m%d-%H%M%S)"
    
    print_success "Stress test completed"
}

run_spike_test() {
    print_header "Running Spike Test (Sudden Load Increase)"
    
    echo "Configuration:"
    echo "  Phase 1: 10 users for 2 minutes (baseline)"
    echo "  Phase 2: 100 users for 3 minutes (spike)"
    echo "  Phase 3: 10 users for 2 minutes (recovery)"
    echo ""
    
    # Phase 1: Baseline
    print_header "Phase 1: Baseline (10 users, 2 min)"
    locust -f load_test.py \
        --host="$RUNEVAULT_ENDPOINT" \
        --users=10 \
        --spawn-rate=5 \
        --run-time=2m \
        --headless \
        --html="$OUTPUT_DIR/spike-test-phase1-$(date +%Y%m%d-%H%M%S).html" \
        --csv="$OUTPUT_DIR/spike-test-phase1-$(date +%Y%m%d-%H%M%S)"
    
    # Phase 2: Spike
    print_header "Phase 2: Spike (100 users, 3 min)"
    locust -f load_test.py \
        --host="$RUNEVAULT_ENDPOINT" \
        --users=100 \
        --spawn-rate=20 \
        --run-time=3m \
        --headless \
        --html="$OUTPUT_DIR/spike-test-phase2-$(date +%Y%m%d-%H%M%S).html" \
        --csv="$OUTPUT_DIR/spike-test-phase2-$(date +%Y%m%d-%H%M%S)"
    
    # Phase 3: Recovery
    print_header "Phase 3: Recovery (10 users, 2 min)"
    locust -f load_test.py \
        --host="$RUNEVAULT_ENDPOINT" \
        --users=10 \
        --spawn-rate=5 \
        --run-time=2m \
        --headless \
        --html="$OUTPUT_DIR/spike-test-phase3-$(date +%Y%m%d-%H%M%S).html" \
        --csv="$OUTPUT_DIR/spike-test-phase3-$(date +%Y%m%d-%H%M%S)"
    
    print_success "Spike test completed"
}

run_custom_test() {
    print_header "Running Custom Load Test"
    
    read -p "Number of users: " USERS
    read -p "Spawn rate (users/sec): " SPAWN_RATE
    read -p "Duration (e.g., 5m, 10m): " DURATION
    
    echo ""
    echo "Configuration:"
    echo "  Users: $USERS"
    echo "  Spawn Rate: $SPAWN_RATE users/sec"
    echo "  Duration: $DURATION"
    echo ""
    
    locust -f load_test.py \
        --host="$RUNEVAULT_ENDPOINT" \
        --users="$USERS" \
        --spawn-rate="$SPAWN_RATE" \
        --run-time="$DURATION" \
        --headless \
        --html="$OUTPUT_DIR/custom-test-$(date +%Y%m%d-%H%M%S).html" \
        --csv="$OUTPUT_DIR/custom-test-$(date +%Y%m%d-%H%M%S)"
    
    print_success "Custom test completed"
}

run_interactive_test() {
    print_header "Running Interactive Test (Web UI)"
    
    echo "Starting Locust web interface..."
    echo "Open browser: http://localhost:8089"
    echo ""
    echo "Press Ctrl+C to stop"
    echo ""
    
    locust -f load_test.py --host="$RUNEVAULT_ENDPOINT"
}

show_results() {
    print_header "Test Results"
    
    if [ ! -d "$OUTPUT_DIR" ] || [ -z "$(ls -A $OUTPUT_DIR)" ]; then
        print_warning "No test results found in $OUTPUT_DIR"
        return
    fi
    
    echo "Available test results:"
    ls -lht "$OUTPUT_DIR" | tail -20
    echo ""
    echo "Results directory: $OUTPUT_DIR"
}

show_menu() {
    echo ""
    echo "Rune-Vault Load Testing Menu"
    echo "=============================="
    echo ""
    echo "Quick Tests:"
    echo "  1) Smoke Test (5 users, 1 min) - Quick validation"
    echo "  2) Baseline Test (25 users, 5 min) - Normal load"
    echo "  3) Sustained Load Test (50 users, 10 min) - Extended test"
    echo ""
    echo "Advanced Tests:"
    echo "  4) Stress Test (100 users, 15 min) - Find limits"
    echo "  5) Spike Test (3 phases) - Test sudden spikes"
    echo "  6) Custom Test - Specify your own parameters"
    echo ""
    echo "Other:"
    echo "  7) Interactive Test (Web UI)"
    echo "  8) Show Test Results"
    echo "  9) Check Requirements"
    echo "  0) Exit"
    echo ""
}

main() {
    print_header "Rune-Vault Load Testing"
    
    # Check requirements first
    check_requirements
    
    while true; do
        show_menu
        read -p "Select option: " OPTION
        
        case $OPTION in
            1) run_smoke_test ;;
            2) run_baseline_test ;;
            3) run_sustained_load_test ;;
            4) run_stress_test ;;
            5) run_spike_test ;;
            6) run_custom_test ;;
            7) run_interactive_test ;;
            8) show_results ;;
            9) check_requirements ;;
            0) 
                echo "Exiting..."
                exit 0
                ;;
            *)
                print_error "Invalid option"
                ;;
        esac
    done
}

# Run main function
main
