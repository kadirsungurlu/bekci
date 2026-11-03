#!/usr/bin/env python3
"""Panel istek süreleri (iş 620): istek.py <etiket> <port> <monitör sayısı> -> JSON satırları."""
import http.client, json, statistics, sys, time
tag, port, n = sys.argv[1], int(sys.argv[2]), int(sys.argv[3])
H = {"X-Uptime": "1", "Content-Type": "application/json"}
def req(m, p, body=None):
    c = http.client.HTTPConnection("127.0.0.1", port, timeout=300)
    t0 = time.time()
    c.request(m, p, json.dumps(body) if body is not None else None, H)
    r = c.getresponse(); d = r.read()
    if r.getheader("Set-Cookie"):
        H["Cookie"] = r.getheader("Set-Cookie").split(";")[0]
    return r.status, d, (time.time() - t0) * 1000
cred = {"username": "olcum", "password": "Olcum-Sifre-620!"}
st, d, _ = req("POST", "/api/auth/setup", cred)
if st >= 300:
    st, d, _ = req("POST", "/api/auth/login", cred)
assert st < 300, (st, d[:200])
mid = n // 2 + 1
ids = [1 + i * n // 50 for i in range(min(50, n))]
st, d, _ = req("POST", "/api/status-pages", {"slug": "olcum", "title": "Olcum", "sections": [{"title": "A", "monitors": [{"id": i} for i in ids]}], "published": True, "bar_range": "90d"})
page_ok = st < 300
if not page_ok:
    print(json.dumps({"tag": tag, "hata_sayfa": st, "yanit": d[:300].decode(errors="replace")}))
for name, path, reps in [
    ("durum sayfasi ilk", "/api/public/pages/olcum", 1),
    ("durum sayfasi onbellekli", "/api/public/pages/olcum", 5),
    ("ozet /api/summary", "/api/summary", 5),
    ("monitor listesi", "/api/monitors", 5),
    ("olaylar", "/api/incidents?limit=50", 5),
    ("topoloji", "/api/topology", 5),
    ("monitor ayrinti", f"/api/monitors/{mid}", 5),
    ("son kontroller", f"/api/monitors/{mid}/beats?limit=50", 5),
    ("grafik 24s ham", f"/api/monitors/{mid}/series?range=24h", 5),
    ("grafik 7g", f"/api/monitors/{mid}/series?range=7d", 5),
    ("grafik 30g", f"/api/monitors/{mid}/series?range=30d", 5),
    ("grafik 90g", f"/api/monitors/{mid}/series?range=90d", 5),
]:
    t, size, code = [], 0, 0
    for _ in range(reps):
        code, d, ms = req("GET", path)
        t.append(ms); size = len(d)
    print(json.dumps({"tag": tag, "istek": name, "ms": round(statistics.median(t), 1), "kb": round(size / 1024), "kod": code}), flush=True)
