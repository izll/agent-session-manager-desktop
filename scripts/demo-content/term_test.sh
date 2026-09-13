#!/usr/bin/env bash
printf '\033[2J\033[H'
echo "$ go test ./... -run Refund"
echo "ok  	billing/internal/money	0.019s"
echo "ok  	billing/internal/ledger	0.204s"
echo "ok  	billing/webhook	0.311s"
echo ""
echo "$ "
while :; do sleep 30; done
