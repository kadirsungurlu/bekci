#!/bin/sh
# Gün sayısından eski journald kayıtlarını siler.
set -eu
gun="$BEKCI_P_GUN"
case "$gun" in
'' | *[!0-9]*) echo "gecersiz gun" >&2; exit 2 ;;
esac
journalctl --vacuum-time="${gun}d"
journalctl --disk-usage
