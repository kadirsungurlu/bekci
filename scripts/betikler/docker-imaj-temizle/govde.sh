#!/bin/sh
# Verilen saatten eski sarkan imajları siler. -a, system prune ve volume yok.
set -eu
saat="$BEKCI_P_SAAT"
case "$saat" in
'' | *[!0-9]*) echo "gecersiz saat" >&2; exit 2 ;;
esac
command -v docker >/dev/null 2>&1 || { echo "docker yok" >&2; exit 3; }
docker image prune -f --filter "until=${saat}h"
