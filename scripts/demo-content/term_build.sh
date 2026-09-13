#!/usr/bin/env bash
printf '\033[2J\033[H'
echo "$ npm run dev"
echo ""
echo "  VITE v8.2.2  ready in 612 ms"
echo ""
echo "  ➜  Local:   http://localhost:5173/"
echo "  ➜  press h + enter to show help"
echo ""
while :; do printf "  %s [vite] hmr update /src/lib/Cart.svelte\n" "$(date +%H:%M:%S)"; sleep 11; done
