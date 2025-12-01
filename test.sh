#!/bin/bash

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

API_URL="${API_URL:-http://localhost:8080}"
TEST_EMAIL="test_$(date +%s)@example.com"
TEST_PASSWORD="TestPass123!"
TEST_NAME="Test User"

echo -e "${YELLOW}🧪 Budget API - Test Suite${NC}"
echo "API URL: $API_URL"
echo "=========================================="

echo -e "\n${YELLOW}1. Health Check${NC}"
curl -s "$API_URL/health" | grep -q "ok" && echo -e "${GREEN}✓ PASSED${NC}" || echo -e "${RED}✗ FAILED${NC}"

echo -e "\n${YELLOW}2. User Signup${NC}"
SIGNUP_RESPONSE=$(curl -s -X POST "$API_URL/api/v1/auth/signup" \
    -H "Content-Type: application/json" \
    -d "{\"email\":\"$TEST_EMAIL\",\"password\":\"$TEST_PASSWORD\",\"name\":\"$TEST_NAME\"}")

if echo "$SIGNUP_RESPONSE" | grep -q "access_token"; then
    echo -e "${GREEN}✓ Signup successful${NC}"
    ACCESS_TOKEN=$(echo "$SIGNUP_RESPONSE" | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4)
    USER_ID=$(echo "$SIGNUP_RESPONSE" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
    echo "Token: ${ACCESS_TOKEN:0:50}..."
else
    echo -e "${RED}✗ Signup failed${NC}"
    echo "$SIGNUP_RESPONSE"
    exit 1
fi

echo -e "\n${YELLOW}3. User Login${NC}"
LOGIN_RESPONSE=$(curl -s -X POST "$API_URL/api/v1/auth/login" \
    -H "Content-Type: application/json" \
    -d "{\"email\":\"$TEST_EMAIL\",\"password\":\"$TEST_PASSWORD\"}")
echo "$LOGIN_RESPONSE" | grep -q "access_token" && echo -e "${GREEN}✓ PASSED${NC}" || echo -e "${RED}✗ FAILED${NC}"

echo -e "\n${YELLOW}4. Get User Profile${NC}"
PROFILE_RESPONSE=$(curl -s -X GET "$API_URL/api/v1/user/profile" \
    -H "Authorization: Bearer $ACCESS_TOKEN")
echo "$PROFILE_RESPONSE" | grep -q "$TEST_EMAIL" && echo -e "${GREEN}✓ PASSED${NC}" || echo -e "${RED}✗ FAILED${NC}"

echo -e "\n${YELLOW}5. Create Budget${NC}"
BUDGET_RESPONSE=$(curl -s -X POST "$API_URL/api/v1/budgets" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $ACCESS_TOKEN" \
    -d '{"name":"Test Budget"}')

if echo "$BUDGET_RESPONSE" | grep -q "id"; then
    echo -e "${GREEN}✓ Budget creation successful${NC}"
    BUDGET_ID=$(echo "$BUDGET_RESPONSE" | grep -o '"id":"[^"]*"' | cut -d'"' -f4)
    echo "Budget ID: $BUDGET_ID"
else
    echo -e "${RED}✗ Budget creation failed${NC}"
fi

echo -e "\n${YELLOW}6. Get All Budgets${NC}"
BUDGETS_RESPONSE=$(curl -s -X GET "$API_URL/api/v1/budgets" \
    -H "Authorization: Bearer $ACCESS_TOKEN")
echo "$BUDGETS_RESPONSE" | grep -q "Test Budget" && echo -e "${GREEN}✓ PASSED${NC}" || echo -e "${RED}✗ FAILED${NC}"

if [ ! -z "$BUDGET_ID" ]; then
    echo -e "\n${YELLOW}7. Update Budget Data${NC}"
    BUDGET_DATA='{"data": {"budgetTitle": "Test Budget Updated","people": [],"charges": [],"projects": [],"yearlyData": {}}}'
    UPDATE_RESPONSE=$(curl -s -X PUT "$API_URL/api/v1/budgets/$BUDGET_ID/data" \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer $ACCESS_TOKEN" \
        -d "$BUDGET_DATA")
    echo "$UPDATE_RESPONSE" | grep -q "version" && echo -e "${GREEN}✓ PASSED${NC}" || echo -e "${RED}✗ FAILED${NC}"
fi

echo -e "\n${GREEN}🎉 All tests completed!${NC}"
echo ""
echo "Test User: $TEST_EMAIL"
echo "Password: $TEST_PASSWORD"
if [ ! -z "$BUDGET_ID" ]; then
    echo "Budget ID: $BUDGET_ID"
fi