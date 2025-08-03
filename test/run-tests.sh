#!/bin/bash

# Tapio Test Runner
# Executes test scenarios and monitors pattern detection

TEST_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

run_scenario() {
    local scenario=$1
    echo "
🧪 Running test scenario: $scenario
=================================="
    
    # Apply the scenario
    kubectl apply -f $TEST_DIR/scenarios/$scenario.yaml
    
    # Wait for events to be generated
    echo "⏱️  Waiting 60 seconds for pattern detection..."
    sleep 60
    
    # Check for detected patterns
    echo "📊 Checking correlation service for patterns..."
    kubectl logs -n tapio-system deployment/correlation-service --tail=100 | \
        grep -E "(High confidence correlation detected|pattern|Pattern detected)" || \
        echo "No patterns detected yet"
    
    # Cleanup
    echo "🧹 Cleaning up..."
    kubectl delete -f $TEST_DIR/scenarios/$scenario.yaml --force --grace-period=0 2>/dev/null
    
    echo ""
}

watch_patterns() {
    echo "👁️  Watching for pattern detection..."
    echo "Press Ctrl+C to stop"
    kubectl logs -n tapio-system deployment/correlation-service -f | \
        grep -E "(pattern|correlation|confidence|detected)" --color=always
}

# Main
case "${1:-help}" in
    oom)
        run_scenario "oom-killer"
        ;;
    crash)
        run_scenario "crash-loop"
        ;;
    watch)
        watch_patterns
        ;;
    all)
        for scenario in oom-killer crash-loop; do
            run_scenario "$scenario"
        done
        ;;
    help|*)
        echo "Tapio Test Runner
Usage: $0 [command]

Commands:
  oom      - Test OOM kill pattern detection
  crash    - Test crash loop pattern detection
  watch    - Watch correlation logs for patterns
  all      - Run all test scenarios
  help     - Show this help
"
        ;;
esac