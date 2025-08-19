#!/bin/bash

# Setup Git hooks for Tapio project

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${GREEN}Setting up Tapio Git hooks...${NC}"

# Configure git to use our hooks directory
git config core.hooksPath .githooks

echo -e "${GREEN}✅ Git hooks configured${NC}"
echo -e "${YELLOW}Pre-commit hook will now enforce:${NC}"
echo -e "  • No map[string]interface{} in public APIs"
echo -e "  • No TODOs, FIXMEs, or stub functions"
echo -e "  • Code must be formatted"
echo -e "  • Code must build"
echo -e "  • Tests must pass"

echo -e "\n${YELLOW}To bypass hooks in emergency (NOT RECOMMENDED):${NC}"
echo -e "  git commit --no-verify"
echo -e "\n${RED}WARNING: Bypassing hooks will likely cause CI/CD failures${NC}"