#!/bin/sh
# Bildirimdeki izinli listeden bir servisi yeniden başlatır.
set -eu
servis="$BEKCI_P_SERVIS"
systemctl restart -- "$servis"
systemctl is-active -- "$servis"
