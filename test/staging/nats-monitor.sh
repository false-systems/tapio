#!/bin/bash

echo "📊 NATS Real-time Monitor"
echo "========================"

# Check if nats CLI is available
if ! command -v nats &> /dev/null; then
    echo "Error: nats CLI not installed"
    echo "Install with: brew install nats-io/nats-tools/nats"
    exit 1
fi

# Function to display stream info
show_stream_info() {
    echo -e "\n📈 Stream: OBSERVATIONS"
    nats stream info OBSERVATIONS --json 2>/dev/null | jq -r '
        "Messages: \(.state.messages)",
        "Bytes: \(.state.bytes)",
        "First Seq: \(.state.first_seq)",
        "Last Seq: \(.state.last_seq)",
        "Consumers: \(.state.consumer_count)"
    ' 2>/dev/null || echo "Stream not found"
}

# Function to show consumer info
show_consumer_info() {
    echo -e "\n👥 Consumers:"
    nats consumer ls OBSERVATIONS 2>/dev/null || echo "No consumers found"
    
    # Show detailed info for loader consumer if exists
    if nats consumer info OBSERVATIONS loader &>/dev/null; then
        echo -e "\n📦 Loader Consumer:"
        nats consumer info OBSERVATIONS loader --json 2>/dev/null | jq -r '
            "Pending: \(.num_pending)",
            "Delivered: \(.delivered.stream_seq)",
            "Ack Floor: \(.ack_floor.stream_seq)",
            "Redelivered: \(.num_redelivered)"
        ' 2>/dev/null
    fi
}

# Function to monitor events
monitor_events() {
    echo -e "\n📡 Monitoring events (Ctrl+C to stop)..."
    echo "----------------------------------------"
    
    # Subscribe to all observation subjects
    nats sub "observations.>" --queue monitor 2>/dev/null | while read -r line; do
        # Parse and format the output
        if [[ $line == *"Received on"* ]]; then
            subject=$(echo $line | grep -oP '(?<=Received on ").*(?=")')
            echo -e "\n🔵 Subject: $subject"
        elif [[ $line != *"Subscribing"* ]] && [[ ! -z "$line" ]]; then
            # Try to parse as JSON and format
            echo "$line" | jq -C '.' 2>/dev/null || echo "$line"
        fi
    done
}

# Main menu
while true; do
    echo -e "\n🎛️  NATS Monitor Menu"
    echo "1) Show stream info"
    echo "2) Show consumer info"
    echo "3) Monitor live events"
    echo "4) Publish test event"
    echo "5) View stream messages"
    echo "6) Exit"
    echo -n "Select option: "
    read option
    
    case $option in
        1)
            show_stream_info
            ;;
        2)
            show_consumer_info
            ;;
        3)
            monitor_events
            ;;
        4)
            echo "Publishing test event..."
            go run /Users/yair/projects/tapio/test/e2e/publish_test_events.go 2>/dev/null || echo "Failed to publish"
            ;;
        5)
            echo -e "\n📜 Recent messages:"
            nats stream get OBSERVATIONS --last 5 2>/dev/null || echo "No messages found"
            ;;
        6)
            echo "Exiting..."
            exit 0
            ;;
        *)
            echo "Invalid option"
            ;;
    esac
done