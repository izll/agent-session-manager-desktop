#!/usr/bin/env bash
E=$'\033'; R="${E}[0m"; GREY="${E}[38;5;245m"; GREEN="${E}[38;5;114m"
printf '\033[2J\033[H'
printf "${GREY}\$${R} go test ./... -run Refund\n"
printf "${GREEN}ok${R}  \tbilling/internal/money\t0.019s\n"
printf "${GREEN}ok${R}  \tbilling/internal/ledger\t0.204s\n"
printf "${GREEN}ok${R}  \tbilling/webhook\t0.311s\n\n"
printf "${GREY}\$${R} "
while :; do sleep 30; done
