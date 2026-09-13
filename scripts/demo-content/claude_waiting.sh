#!/usr/bin/env bash
E=$'\033'; R="${E}[0m"; DIM="${E}[2m"; B="${E}[1m"
GREEN="${E}[38;5;114m"; ORANGE="${E}[38;5;215m"; BLUE="${E}[38;5;110m"
printf '\033[2J\033[H'
printf "${GREEN}⏺${R} I'd like to run the migration against the staging database to check\n"
printf "  the index actually helps before committing to it.\n\n"
printf "${BLUE}╭────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────╮${R}\n"
printf "${BLUE}│${R} ${B}Bash${R}(psql \$STAGING_URL -f migrations/014_add_index.sql)                                                                                   ${BLUE}│${R}\n"
printf "${BLUE}│${R} ${DIM}Add a composite index on (tenant_id, created_at)${R}                                                                                           ${BLUE}│${R}\n"
printf "${BLUE}│${R}                                                                                                                                            ${BLUE}│${R}\n"
printf "${BLUE}│${R} ${B}Do you want to proceed?${R}                                                                                                                    ${BLUE}│${R}\n"
printf "${BLUE}│${R} ${ORANGE}❯ 1. Yes${R}                                                                                                                                   ${BLUE}│${R}\n"
printf "${BLUE}│${R}   2. Yes, and don't ask again for psql commands                                                                                            ${BLUE}│${R}\n"
printf "${BLUE}│${R}   3. No, tell Claude what to do differently ${DIM}(esc)${R}                                                                                          ${BLUE}│${R}\n"
printf "${BLUE}╰────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────╯${R}\n"
while :; do sleep 30; done
