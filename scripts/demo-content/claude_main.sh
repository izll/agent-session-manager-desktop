#!/usr/bin/env bash
# A Claude Code kimenetenek alakja: szinezett jelolok, keretezett bevitel,
# statuszsor. ANSI-kodokkal, mert e nelkul a kep fekete-feher es nem az,
# amit a felhasznalo lat.
E=$'\033'
DIM="${E}[2m"; R="${E}[0m"; B="${E}[1m"
GREEN="${E}[38;5;114m"; ORANGE="${E}[38;5;215m"; BLUE="${E}[38;5;110m"
GREY="${E}[38;5;245m"; RED="${E}[38;5;174m"; PURPLE="${E}[38;5;140m"

printf '\033[2J\033[H'
printf "${GREY}> the refund rounding is wrong for JPY — it invents a minor unit${R}\n\n"
printf "${GREEN}⏺${R} I'll look at how the rounding is applied, then at what the currency\n"
printf "  table says.\n\n"
printf "${GREEN}⏺${R} ${B}Read${R}(internal/money/round.go)\n"
printf "  ${DIM}⎿${R}  Read 84 lines\n\n"
printf "${GREEN}⏺${R} ${B}Search${R}(pattern: \"minorUnits|Decimal\", path: \"internal/money\")\n"
printf "  ${DIM}⎿${R}  Found 11 matches across 3 files\n\n"
printf "${GREEN}⏺${R} The rounding is hardcoded to two decimal places:\n\n"
printf "    ${PURPLE}return${R} amount.Round(${ORANGE}2${R})\n\n"
printf "  JPY has no minor unit, so 4200 JPY becomes 4200.00 and the comparison\n"
printf "  against the ledger fails by a cent that does not exist.\n\n"
printf "${GREEN}⏺${R} ${B}Update${R}(internal/money/round.go)\n"
printf "  ${DIM}⎿${R}  Updated with 9 additions and 2 removals\n"
printf "     ${GREY}12${R} ${RED}-  return amount.Round(2)${R}\n"
printf "     ${GREY}13${R} ${GREEN}+  units, ok := minorUnits[currency]${R}\n"
printf "     ${GREY}14${R} ${GREEN}+  if !ok {${R}\n"
printf "     ${GREY}15${R} ${GREEN}+      return Decimal{}, fmt.Errorf(\"unknown currency %%q\", currency)${R}\n"
printf "     ${GREY}16${R} ${GREEN}+  }${R}\n"
printf "     ${GREY}17${R} ${GREEN}+  return amount.Round(units), nil${R}\n\n"
printf "${GREEN}⏺${R} ${B}Bash${R}(go test ./internal/money/...)\n"
printf "  ${DIM}⎿${R}  ${GREEN}ok${R}  \tbilling/internal/money\t0.019s\n\n"
printf "${GREEN}⏺${R} The table now drives the rounding, and an unknown currency is an error\n"
printf "  rather than a silent two-place guess. Three call sites needed the extra\n"
printf "  return value; all updated.\n\n"
printf "  Worth deciding: should an unknown currency fall back to two places\n"
printf "  instead of failing? That would keep older integrations working.\n\n"
printf "${BLUE}╭──────────────────────────────────────────────────────────────────────────╮${R}\n"
printf "${BLUE}│${R} ${GREY}>${R}                                                                        ${BLUE}│${R}\n"
printf "${BLUE}╰──────────────────────────────────────────────────────────────────────────╯${R}\n"
printf "  ${DIM}⏵⏵ auto mode on · 2 shells · ← for agents${R}\n"
while :; do sleep 30; done
