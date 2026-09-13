#!/usr/bin/env bash
E=$'\033'; R="${E}[0m"; DIM="${E}[2m"; B="${E}[1m"
GREEN="${E}[38;5;114m"; CYAN="${E}[38;5;80m"; GREY="${E}[38;5;245m"
printf '\033[2J\033[H'
printf "${GREY}\$${R} npm run dev\n\n"
printf "  ${GREEN}${B}VITE v8.2.2${R}  ${DIM}ready in 612 ms${R}\n\n"
printf "  ${GREEN}➜${R}  ${B}Local${R}:   ${CYAN}http://localhost:5173/${R}\n"
printf "  ${GREEN}➜${R}  ${DIM}press h + enter to show help${R}\n\n"
while :; do
  printf "  ${DIM}%s${R} ${CYAN}[vite]${R} hmr update ${DIM}/src/lib/Cart.svelte${R}\n" "$(date +%H:%M:%S)"
  sleep 11
done
