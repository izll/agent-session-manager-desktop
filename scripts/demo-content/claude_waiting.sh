#!/usr/bin/env bash
printf '\033[2J\033[H'
cat <<'BODY'
● I'd like to run the migration against the staging database to check
  the index actually helps before committing to it.

  Bash(psql $STAGING_URL -f migrations/014_add_index.sql)
  Add a composite index on (tenant_id, created_at)

Do you want to proceed?
❯ 1. Yes
  2. Yes, and don't ask again for psql commands
  3. No, tell Claude what to do differently (esc)
BODY
while :; do sleep 30; done
