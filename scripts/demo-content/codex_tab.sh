#!/usr/bin/env bash
printf '\033[2J\033[H'
cat <<'BODY'
› add a retry with backoff to the webhook sender

• Read webhook/sender.go
• Search "maxRetries|backoff" in webhook
• Edited webhook/sender.go (+18 -4)

  The three retries fired back to back, so a partner that was already
  struggling got all of them inside 40ms. Now it waits 1s, 4s, 16s with
  jitter, and gives up after the fourth.

• Ran go test ./webhook/... → ok (0.31s)

Worth noting: the jitter uses math/rand without a seed, which is fine
here but will repeat across restarts. Say the word and I'll switch it
to crypto/rand.
BODY
printf '\n▌ \n'
printf '  gpt-5-codex medium · ~/projects/billing · main\n'
while :; do sleep 30; done
