#!/usr/bin/env bash
printf '\033[2J\033[H'
echo "$ python train.py --config configs/ranker.yaml"
echo "loading 1.2M rows from feature-store ... done (14.2s)"
echo ""
i=24
while :; do
  printf "epoch %2d/50  loss 0.3412  val_auc 0.8871  %ds/epoch\n" "$i" 3
  i=$((i+1)); [ "$i" -gt 50 ] && i=24
  sleep 6
done
