#!/usr/bin/env bash
# Busy allapot: a detektor az "esc to interrupt"-ra ismer ra, ezert a
# statuszsornak ott kell lennie, kulonben a session idle-nek latszik.
E=$'\033'; R="${E}[0m"; DIM="${E}[2m"; B="${E}[1m"
GREEN="${E}[38;5;114m"; ORANGE="${E}[38;5;215m"; GREY="${E}[38;5;245m"
printf '\033[2J\033[H'
printf "${GREY}> migrate the ledger writes onto the new batching API${R}\n\n"
printf "${GREEN}⏺${R} ${B}Read${R}(internal/ledger/writer.go)\n"
printf "  ${DIM}⎿${R}  Read 212 lines\n\n"
printf "${GREEN}⏺${R} ${B}Search${R}(pattern: \"WriteEntry|Flush\", path: \"internal\")\n"
printf "  ${DIM}⎿${R}  Found 34 matches across 9 files\n\n"
printf "${GREEN}⏺${R} Thirty-four call sites, and about half of them write one entry at a time\n"
printf "  inside a loop. Those are the ones worth batching; the rest already hold a\n"
printf "  slice and can pass it straight through.\n\n"
printf "${GREEN}⏺${R} ${B}Update${R}(internal/ledger/writer.go)\n"
printf "  ${DIM}⎿${R}  Updated with 31 additions and 12 removals\n\n"
printf "${GREEN}⏺${R} ${B}Update${R}(internal/ledger/batch.go)\n"
printf "  ${DIM}⎿${R}  Updated with 18 additions\n\n"
printf "${GREEN}⏺${R} ${B}Bash${R}(go build ./... && go test ./internal/ledger/...)\n"
printf "  ${DIM}⎿${R}  Running...\n\n"
SPIN='⠋ ⠙ ⠹ ⠸ ⠼ ⠴ ⠦ ⠧ ⠇ ⠏'
i=0
while :; do
  for c in $SPIN; do
    i=$((i+1))
    printf "\r${ORANGE}%s${R} Batching ledger writes… ${DIM}(%ds · esc to interrupt)${R}  " "$c" "$i"
    sleep 0.12
  done
done
