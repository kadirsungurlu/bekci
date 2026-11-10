#!/bin/sh
# Bekci imajının giriş betiği. Bütün argümanlar uptime programına geçer
# (docker run imaj version, docker run imaj probe ...).
#
# Root başlarsa: ana makineden salt okunur bağlanan cihaz kimliği dosyaları
# (/run/bekci-host/product_uuid, /run/bekci-host/machine-id; Pro cihaza bağlı
# lisans için, bkz. docs/LISANS-TASARIM.md) okunabiliyorsa /run/bekci/cihaz
# altına kopyalanır, sonra program uptime kullanıcısıyla çalışır. product_uuid
# ana makinede yalnız root'a açık olduğundan kopya gerekir. Dosyalar yoksa da
# panel çalışır (cihaz kimliği olmaz).
set -eu

if [ "$(id -u)" = "0" ]; then
	hedef=${BEKCI_CIHAZ_DIZIN:-/run/bekci/cihaz}
	for ad in product_uuid machine-id; do
		kaynak=/run/bekci-host/$ad
		if [ -r "$kaynak" ] && [ -f "$kaynak" ]; then
			mkdir -p "$hedef" && chmod 0755 "$hedef"
			if cp "$kaynak" "$hedef/$ad.yeni" 2>/dev/null; then
				chmod 0444 "$hedef/$ad.yeni"
				mv -f "$hedef/$ad.yeni" "$hedef/$ad"
			else
				rm -f "$hedef/$ad.yeni"
			fi
		fi
	done
	exec su-exec uptime uptime "$@"
fi
exec uptime "$@"
