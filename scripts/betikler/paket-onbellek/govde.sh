#!/bin/sh
# İndirilmiş paket önbelleğini siler; kurulu paketlere dokunmaz.
set -eu
if command -v apt-get >/dev/null 2>&1; then
	apt-get clean
elif command -v dnf >/dev/null 2>&1; then
	dnf clean packages
else
	echo "desteklenen paket yoneticisi yok" >&2
	exit 3
fi
