#!/bin/bash
set -e

echo "🤖 Agent Task Manager"
echo "==================="
echo

# Agent selection
echo "Select Agent:"
echo "1) agent-1"
echo "2) agent-2"
echo "3) agent-3"
echo "4) Custom agent ID"
read -p "Enter choice [1-4]: " agent_choice

case $agent_choice in
    1) AGENT_ID="agent-1";;
    2) AGENT_ID="agent-2";;
    3) AGENT_ID="agent-3";;
    4) read -p "Enter custom agent ID: " AGENT_ID;;
    *) echo "Invalid choice"; exit 1;;
esac

# Component selection
echo
echo "Select Component:"
echo "1) ebpf-sources"
echo "2) correlation-engine"
echo "3) cli"
echo "4) monitoring"
echo "5) Custom component"
read -p "Enter choice [1-5]: " comp_choice

case $comp_choice in
    1) COMPONENT="ebpf-sources";;
    2) COMPONENT="correlation-engine";;
    3) COMPONENT="cli";;
    4) COMPONENT="monitoring";;
    5) read -p "Enter custom component: " COMPONENT;;
    *) echo "Invalid choice"; exit 1;;
esac

# Action selection
echo
echo "Select Action:"
echo "1) implementation"
echo "2) enhancement"
echo "3) bugfix"
echo "4) refactoring"
echo "5) testing"
echo "6) Custom action"
read -p "Enter choice [1-6]: " action_choice

case $action_choice in
    1) ACTION="implementation";;
    2) ACTION="enhancement";;
    3) ACTION="bugfix";;
    4) ACTION="refactoring";;
    5) ACTION="testing";;
    6) read -p "Enter custom action: " ACTION;;
    *) echo "Invalid choice"; exit 1;;
esac

echo
echo "📋 Summary:"
echo "   Agent: $AGENT_ID"
echo "   Component: $COMPONENT"
echo "   Action: $ACTION"
echo

read -p "Proceed? [Y/n] " confirm
if [[ $confirm =~ ^[Nn]$ ]]; then
    echo "Cancelled."
    exit 0
fi

# Call the branch creation script
./scripts/agent-branch.sh "$AGENT_ID" "$COMPONENT" "$ACTION"