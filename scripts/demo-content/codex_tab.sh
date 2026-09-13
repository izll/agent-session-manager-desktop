#!/usr/bin/env bash
E=$'\033'; R="${E}[0m"; DIM="${E}[2m"; B="${E}[1m"
CYAN="${E}[38;5;80m"; GREY="${E}[38;5;245m"; GREEN="${E}[38;5;114m"
printf '\033[2J\033[H'
printf "${GREY}› add a retry with backoff to the webhook sender${R}\n\n"
printf "${CYAN}•${R} Read ${B}webhook/sender.go${R}\n"
printf "${CYAN}•${R} Search ${DIM}\"maxRetries|backoff\" in webhook${R}\n"
printf "${CYAN}•${R} Edited ${B}webhook/sender.go${R} ${GREEN}(+18${R} ${DIM}-4)${R}\n\n"
printf "  The three retries fired back to back, so a partner that was already\n"
printf "  struggling got all of them inside 40ms. Now it waits 1s, 4s, 16s with\n"
printf "  jitter, and gives up after the fourth.\n\n"
printf "${CYAN}•${R} Ran ${B}go test ./webhook/...${R} → ${GREEN}ok${R} ${DIM}(0.31s)${R}\n\n"
printf "  Worth noting: the jitter uses math/rand without a seed, which is fine\n"
printf "  here but will repeat across restarts. Say the word and I'll switch it\n"
printf "  to crypto/rand.\n\n"
case "${1:-0}" in
  1) printf "  ${DIM}reviewed 3 files · 2 suggestions · gpt-5-codex medium${R}\n" ;;
  2) printf "  ${DIM}waiting on the test run before the next edit${R}\n" ;;
  *) printf "  ${DIM}gpt-5-codex medium · ~/projects/billing · main${R}\n" ;;
esac
while :; do sleep 30; done
