#!/usr/bin/env bash
# Valoszeru Claude Code munkamenet: kiirja a beszelgetest, majd varakozik.
printf '\033[2J\033[H'
cat <<'BODY'
> the refund rounding is wrong for JPY — it invents a minor unit

● I'll look at how the rounding is applied, then at what the currency
  table says.

● Read(internal/money/round.go)
  ⎿  Read 84 lines

● Search(pattern: "minorUnits|Decimal", path: "internal/money")
  ⎿  Found 11 matches across 3 files

● The rounding is hardcoded to two decimal places:

    return amount.Round(2)

  JPY has no minor unit, so 4200 JPY becomes 4200.00 and the comparison
  against the ledger fails by a cent that does not exist.

● Update(internal/money/round.go)
  ⎿  Updated with 9 additions and 2 removals
       12 -  return amount.Round(2)
       13 +  units, ok := minorUnits[currency]
       14 +  if !ok {
       15 +      return Decimal{}, fmt.Errorf("unknown currency %q", currency)
       16 +  }
       17 +  return amount.Round(units), nil

● Bash(go test ./internal/money/...)
  ⎿  ok  	billing/internal/money	0.019s

● The table now drives the rounding, and an unknown currency is an error
  rather than a silent two-place guess. Three call sites needed the extra
  return value; all updated.

  Worth deciding: should an unknown currency fall back to two places
  instead of failing? That would keep older integrations working.

BODY
printf '╭──────────────────────────────────────────────────────────────────────╮\n'
printf '│ >                                                                    │\n'
printf '╰──────────────────────────────────────────────────────────────────────╯\n'
printf '  ⏵⏵ auto mode on · 2 shells · ← for agents\n'
while :; do sleep 30; done
