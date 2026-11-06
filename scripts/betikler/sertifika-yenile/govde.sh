#!/bin/sh
# Yenilemesi gelen sertifikaları yeniler ya da Caddy yapılandırmasını yeniden yükler.
set -eu
case "$BEKCI_P_ARAC" in
certbot) certbot renew --non-interactive ;;
caddy) systemctl reload caddy ;;
*) echo "gecersiz arac" >&2; exit 2 ;;
esac
