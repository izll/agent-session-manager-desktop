#!/usr/bin/env bash
E=$'\033'; R="${E}[0m"; DIM="${E}[2m"; GREY="${E}[38;5;245m"; GREEN="${E}[38;5;114m"
printf '\033[2J\033[H'
printf "${GREY}\$${R} python train.py --config configs/ranker.yaml\n"
printf "${DIM}loading 1.2M rows from feature-store ... done (14.2s)${R}\n\n"
i=$(( 24 + ${1:-0} * 7 ))
while :; do
  printf "epoch ${GREEN}%2d${R}/50  loss ${DIM}0.3412${R}  val_auc ${GREEN}0.8871${R}  ${DIM}3s/epoch${R}\n" "$i"
  i=$((i+1)); [ "$i" -gt 50 ] && i=$(( 24 + ${1:-0} * 7 ))
  sleep 6
done
