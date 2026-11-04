package api

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/betik"
)

// TestE2EBetikPanel Playwright (web/e2e/betik.mjs) için panel açar: gerçek
// handler, gerçek PostgreSQL, ileri sarılabilir saat. Üretim kodunda test kapısı
// olmasın diye burada duruyor; adım başına yeni TOTP kodu gerektiğinden gerçek
// saatle bu akış dakikalar sürerdi. Yalnız BEKCI_E2E_ADDR verilince çalışır.
//
//	BEKCI_E2E_ADDR=127.0.0.1:8081 UPTIME_TEST_PG=... go test -timeout 30m -run TestE2EBetikPanel ./internal/api
//
// /__e2e/stop çağrılınca biter. Tohum gerçek API'den geçer, kayıt zinciri geçerlidir.
func TestE2EBetikPanel(t *testing.T) {
	addr := os.Getenv("BEKCI_E2E_ADDR")
	if addr == "" {
		t.Skip("BEKCI_E2E_ADDR yok")
	}
	dist := os.Getenv("BEKCI_E2E_DIST")
	if dist == "" {
		dist = "../../web/dist"
	}
	if _, err := os.Stat(filepath.Join(dist, "index.html")); err != nil {
		t.Fatalf("arayüz derlenmemiş: %v", err)
	}
	x := setupScriptsStatic(t, os.DirFS(dist), -120*time.Hour)
	h := &e2eHarness{x: x, stop: make(chan struct{}), hosts: map[string]*e2eHost{}, secrets: map[string]string{"kadir": x.secret}}
	h.seed()

	mux := http.NewServeMux()
	mux.HandleFunc("/__e2e/", h.serve)
	mux.Handle("/", x.s.Handler())
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	fmt.Println("e2e panel hazır:", addr)
	<-h.stop
}

type e2eHost struct {
	name  string
	probe createdProbeT
	key   []byte
	seq   int64
	prev  string
}

type e2eHarness struct {
	x       *sEnv
	mu      sync.Mutex
	stop    chan struct{}
	once    sync.Once
	hosts   map[string]*e2eHost
	secrets map[string]string
	ayse    *env
}

// use x'in tek ajanlı yardımcılarını bu ajana çevirir.
func (h *e2eHarness) use(n *e2eHost) {
	h.x.probe, h.x.host = n.probe, n.key
	h.x.receiptSeq, h.x.receiptPrev = n.seq, n.prev
}

func (h *e2eHarness) addHost(key, name, caps string, approve bool) {
	x := h.x
	x.t.Helper()
	var cp struct {
		Probe struct {
			ID int64 `json:"id"`
		} `json:"probe"`
		Token string `json:"token"`
	}
	x.mustDo("POST", "/api/probes", map[string]any{"name": name}, &cp, 201)
	n := &e2eHost{name: name, probe: createdProbeT{cp.Probe.ID, cp.Token}, key: genKey(x.t)}
	h.hosts[key] = n
	h.use(n)
	if caps == "" { // eski ajan: yetenek başlığı yok
		x.rawReq("GET", "/api/probe/jobs", bearer(n.probe.Token), nil)
		return
	}
	hdr := bearer(n.probe.Token)
	hdr["X-Probe-Scripts"] = caps
	hdr["X-Probe-Script-Key"] = betik.EncodePublicKey(pubOf(n.key))
	x.clk.advance(301 * time.Second)
	x.rawReq("GET", "/api/probe/jobs", hdr, nil)
	if !approve {
		return
	}
	var hosts []scriptHostView
	x.mustDo("GET", "/api/scripts/hosts", nil, &hosts, 200)
	for _, v := range hosts {
		if v.ProbeID == n.probe.ID {
			x.mustDo("POST", fmt.Sprintf("/api/scripts/hosts/%d/approve", n.probe.ID), map[string]any{"host_key": v.PendingKey, "code": x.step()}, nil, 200)
		}
	}
}

// signBundle başlangıç betiğini ya da verilen bildirimi imzalar.
func (h *e2eHarness) signBundle(dir, id string, ver int64, ov func(*betik.Manifest)) (json.RawMessage, []byte) {
	x := h.x
	x.t.Helper()
	body := []byte("#!/bin/sh\necho merhaba\n")
	now := x.clk.now().Unix()
	m := betik.Manifest{Schema: 1, ID: id, Version: ver, Interpreter: "sh", RunAs: "nobody", Platforms: []string{"linux"},
		Approval: betik.ApprovalNone, Groups: []string{"temizlik"}, Params: []betik.Param{}, TimeoutSec: 60, OutputMax: 65536, MaxPerDay: 10}
	if dir != "" {
		root := filepath.Join("../../scripts/betikler", dir)
		raw, err := os.ReadFile(filepath.Join(root, "manifest.json"))
		if err != nil {
			x.t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &m); err != nil {
			x.t.Fatal(err)
		}
		if body, err = os.ReadFile(filepath.Join(root, "govde.sh")); err != nil {
			x.t.Fatal(err)
		}
		m.ID, m.Version = id, ver
	}
	m.SHA256, m.NotBefore, m.NotAfter = betik.Digest(body), now-10, now+90*86400
	if ov != nil {
		ov(&m)
	}
	p, err := json.Marshal(m)
	if err != nil {
		x.t.Fatal(err)
	}
	env, err := betik.Sign(x.yayin, betik.KindManifest, p)
	if err != nil {
		x.t.Fatal(err)
	}
	return env, body
}

func (h *e2eHarness) upload(dir, id string, ver int64, ov func(*betik.Manifest)) {
	env, body := h.signBundle(dir, id, ver, ov)
	h.x.mustDo("POST", "/api/scripts", map[string]any{"manifest": env, "body": body, "code": h.x.step()}, nil, 201)
}

// start bir sonraki çalıştırmayı başlatır; sıklık sınırına takılmasın diye saat ileri alınır.
func (h *e2eHarness) start(c *env, secret, id string, ver int64, host string, params map[string]any) runView {
	x := h.x
	x.t.Helper()
	x.clk.advance(7 * time.Hour)
	h.use(h.hosts[host])
	code, v, raw := x.start(c, id, ver, secret, map[string]any{"params": params})
	if code >= 300 {
		x.t.Fatalf("başlatma %s: %d %s", id, code, raw)
	}
	return v
}

// finish ajanın işi alıp makbuz yazmasını taklit eder.
func (h *e2eHarness) finish(runID, host, status string, exit int, output string) {
	x := h.x
	x.t.Helper()
	n := h.hosts[host]
	h.use(n)
	v := runView{RunID: runID}
	x.probeEvent(n.probe.Token, map[string]any{"kind": "accepted", "run_id": runID, "time": x.clk.now().Unix()})
	if status == "" {
		return
	}
	env := x.receipt(v, func(r *betik.Receipt) {
		r.Status, r.ExitCode = status, exit
		r.OutputSHA256 = betik.Digest([]byte(output))
		r.Seq, r.Prev = n.seq+1, n.prev
	})
	parsed, err := betik.ParseEnvelope(env)
	if err != nil {
		x.t.Fatal(err)
	}
	if c := x.probeEvent(n.probe.Token, map[string]any{"kind": "receipt", "run_id": runID, "receipt": env, "output": []byte(output)}); c != 200 {
		x.t.Fatalf("makbuz: %d", c)
	}
	n.seq, n.prev = n.seq+1, parsed.PayloadDigest()
}

func e2eLongOutput(lines int) string {
	var b strings.Builder
	for i := 1; i <= lines; i++ {
		fmt.Fprintf(&b, "[%03d] /var/lib/docker/overlay2/4f7c2d9be1a0c6/merged/usr/share/doc/paket-%d silindi (%d MB)\n", i, i, i%37+1)
	}
	b.WriteString("uzun-tek-satir:" + strings.Repeat("0123456789abcdef", 40) + "\n")
	return b.String()
}

// seed gerçek API üzerinden gerçekçi veri kurar: beş sunucu, dokuz betik, her durumdan çalıştırma.
func (h *e2eHarness) seed() {
	x := h.x
	x.addKeys()
	ayse, ayseSec := x.secondAdmin()
	h.ayse, h.secrets["ayse"] = ayse, ayseSec

	h.addHost("web", "web-01.istanbul", "schemas=1;state=on", true)
	h.addHost("db", "veritabani-yedek-sunucusu-frankfurt-02.ornek-sirket.com.tr", "schemas=1;state=on", true)
	h.addHost("kapali", "kenar-03.izmir", "schemas=1;state=off;reason=disabled", false)
	h.addHost("yeni", "yeni-sunucu-04.ankara", "schemas=1;state=on", false)
	h.addHost("eski", "eski-ajan-05", "", false)

	for _, id := range []string{"journal-temizle", "paket-onbellek", "docker-derleme-temizle", "docker-imaj-temizle", "servis-yeniden", "servis-yeniden-web", "sertifika-yenile"} {
		h.upload(id, id, 1, nil)
	}
	h.upload("journal-temizle", "journal-temizle", 2, nil)
	h.upload("", "postgresql-yedek-dogrulama-ve-uzak-depo-esitleme-gunluk", 1, func(m *betik.Manifest) {
		m.Approval, m.Groups = betik.ApprovalPanel, []string{"veritabani"}
		m.Params = []betik.Param{{Name: "gun", Type: "int", Min: e2eI(1), Max: e2eI(30)}, {Name: "kuru", Type: "bool"}}
	})
	h.upload("", "eski-kayit-arsivle", 1, func(m *betik.Manifest) { m.NotAfter = x.clk.now().Unix() + 3600 })
	h.upload("", "kaldirilan-gecici-yama", 1, nil)
	rv, err := json.Marshal(betik.Revoke{Schema: 1, ID: "kaldirilan-gecici-yama", BelowVersion: 2})
	if err != nil {
		x.t.Fatal(err)
	}
	renv, err := betik.Sign(x.yayin, betik.KindRevoke, rv)
	if err != nil {
		x.t.Fatal(err)
	}
	x.mustDo("POST", "/api/scripts/revokes", map[string]any{"revoke": json.RawMessage(renv), "code": x.step()}, nil, 201)

	gun := map[string]any{"gun": 14}
	saat := map[string]any{"saat": 168}
	none := map[string]any{}
	r := h.start(x.env, x.secret, "journal-temizle", 2, "web", gun)
	h.finish(r.RunID, "web", betik.StatusOK, 0, "Vacuuming done, freed 1.2G of archived journals.\n")

	r = h.start(x.env, x.secret, "paket-onbellek", 1, "db", none)
	h.finish(r.RunID, "db", betik.StatusFailed, 3, "dnf: önbellek kilitli, işlem yarıda kaldı\n"+e2eLongOutput(12))

	r = h.start(x.env, x.secret, "docker-imaj-temizle", 1, "web", saat)
	x.clk.advance(31 * time.Second)
	ayse.mustDo("POST", "/api/scripts/runs/"+r.RunID+"/approve", map[string]any{"code": x.code(ayseSec)}, nil, 200)
	h.finish(r.RunID, "web", betik.StatusOK, 0, e2eLongOutput(320))

	r = h.start(x.env, x.secret, "journal-temizle", 2, "db", gun) // ajan sıklık sınırıyla reddetti
	x.probeEvent(h.hosts["db"].probe.Token, map[string]any{"kind": "rejected", "run_id": r.RunID, "code": betik.CodeRateInterval, "time": x.clk.now().Unix()})

	r = h.start(x.env, x.secret, "paket-onbellek", 1, "web", none) // süresi doldu
	x.clk.advance(2 * time.Hour)
	x.s.scriptTick(x.t.Context(), x.t.TempDir())

	r = h.start(x.env, x.secret, "docker-derleme-temizle", 1, "web", saat) // yönetici reddetti
	x.mustDo("POST", "/api/scripts/runs/"+r.RunID+"/reject", map[string]any{}, nil, 200)

	r = h.start(x.env, x.secret, "journal-temizle", 2, "web", gun) // kuyruktayken iptal
	x.mustDo("POST", "/api/scripts/runs/"+r.RunID+"/cancel", map[string]any{}, nil, 200)

	h.start(x.env, x.secret, "servis-yeniden", 1, "web", map[string]any{"servis": "chronyd"})                                          // imza bekliyor
	h.start(x.env, x.secret, "docker-derleme-temizle", 1, "db", saat)                                                                  // ayse onaylayacak
	h.start(ayse, ayseSec, "postgresql-yedek-dogrulama-ve-uzak-depo-esitleme-gunluk", 1, "db", map[string]any{"gun": 7, "kuru": true}) // kadir onaylayacak

	x.clk.off.Store(0)
}

func e2eI(v int64) *int64 { return &v }

func (h *e2eHarness) serve(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	x := h.x
	q := r.URL.Query()
	switch strings.TrimPrefix(r.URL.Path, "/__e2e/") {
	case "totp":
		x.clk.advance(31 * time.Second)
		writeJSON(w, 200, map[string]string{"code": x.code(h.secrets[q.Get("user")])})
	case "agent": // ?run=&host=&status=&exit=&reject=
		n := h.hosts[q.Get("host")]
		if n == nil {
			http.Error(w, "host", 400)
			return
		}
		h.use(n)
		x.poll()
		if code := q.Get("reject"); code != "" {
			x.probeEvent(n.probe.Token, map[string]any{"kind": "rejected", "run_id": q.Get("run"), "code": code, "time": x.clk.now().Unix()})
		} else {
			exit, _ := strconv.Atoi(q.Get("exit"))
			h.finish(q.Get("run"), q.Get("host"), q.Get("status"), exit, q.Get("output"))
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	case "bundle": // ?id=&ver=
		ver, _ := strconv.ParseInt(q.Get("ver"), 10, 64)
		env, body := h.signBundle("", q.Get("id"), ver, nil)
		writeJSON(w, 200, map[string]any{"manifest": json.RawMessage(env), "body": body})
	case "ticket": // ?run=
		var d struct {
			Req map[string]any `json:"ticket_request"`
		}
		x.mustDo("GET", "/api/scripts/runs/"+q.Get("run"), nil, &d, 200)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(x.ticket(runView{RunID: q.Get("run")}, d.Req, nil))
	case "verify": // kayıt zinciri çevrimdışı doğrulanır
		x.chain(x.t)
		writeJSON(w, 200, map[string]bool{"ok": true})
	case "stop":
		writeJSON(w, 200, map[string]bool{"ok": true})
		h.once.Do(func() { close(h.stop) })
	default:
		http.NotFound(w, r)
	}
}
