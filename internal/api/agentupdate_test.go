package api

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/kadirsungurlu/bekci/internal/agentupdate"
	"github.com/kadirsungurlu/bekci/internal/servers"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// Testlerde imaj platformu olmayan bir platform kullanılır: panelin kendi
// platformu için çalışan program (test ikilisi) sunulur, onu imzalamak istemeyiz.
const (
	tuOS   = "linux"
	tuArch = "riscv64"
)

// agentUpdateEnv panel sürümü 1.3.1, AgentDir'de imzalı (test anahtarı) bir
// linux/riscv64 ajan programı.
func agentUpdateEnv(t *testing.T, panelVersion string) (*fenv, []byte) {
	t.Helper()
	f := newServersEnv(t)
	f.s.version = panelVersion
	f.s.AgentDir = t.TempDir()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	agentupdate.TestPublicKey = pub
	t.Cleanup(func() { agentupdate.TestPublicKey = nil })
	bin := []byte("yeni-ajan-programi-" + panelVersion)
	if err := os.WriteFile(filepath.Join(f.s.AgentDir, "uptime-"+tuOS+"-"+tuArch), bin, 0o755); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(bin)
	m := agentupdate.Manifest{Version: panelVersion, OS: tuOS, Arch: tuArch, SHA256: hex.EncodeToString(h[:])}
	if agentupdate.IsVersion(panelVersion) {
		sig, err := agentupdate.Sign(priv, m)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(agentupdate.Signed{Manifest: m, Sig: base64.StdEncoding.EncodeToString(sig)})
		if err := os.WriteFile(filepath.Join(f.s.AgentDir, "uptime-"+tuOS+"-"+tuArch+".sig"), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return f, bin
}

// agentCall ajan gibi istek atar (sürüm ve platform başlıklarıyla).
func agentCall(t *testing.T, e *env, method, path, token, version, platform string, body any) (int, []byte) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequestWithContext(context.Background(), method, e.srv.URL+path, r)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Probe-Version", version)
	if platform != "" {
		req.Header.Set("X-Probe-Platform", platform)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

// offerOf iş listesi yanıtındaki güncelleme teklifi (yoksa nil).
func offerOf(t *testing.T, e *env, token, version, platform string) *agentupdate.Offer {
	t.Helper()
	code, body := agentCall(t, e, "GET", "/api/probe/jobs", token, version, platform, nil)
	if code != 200 {
		t.Fatalf("iş listesi: %d %s", code, body)
	}
	var resp struct {
		Update *agentupdate.Offer `json:"update"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatal(err)
	}
	return resp.Update
}

func probeUpdateState(t *testing.T, e *env, id int64) agentupdate.Status {
	t.Helper()
	var list struct {
		Servers []servers.View `json:"servers"`
	}
	e.mustDo("GET", "/api/servers", nil, &list, 200)
	for _, v := range list.Servers {
		if v.ID == id && v.Update != nil {
			return *v.Update
		}
	}
	t.Fatalf("ajan %d bulunamadı", id)
	return agentupdate.Status{}
}

func TestAgentUpdateFlow(t *testing.T) {
	f, bin := agentUpdateEnv(t, "1.3.1")
	e := f.env
	plat := tuOS + "/" + tuArch
	var cp serverSetup
	e.mustDo("POST", "/api/servers", map[string]any{"name": "Sunucu"}, &cp, 201)
	id := cp.Probe.ID

	// Eski ajan (platform bildirmiyor): kendini güncelleyemez, teklif yok.
	if o := offerOf(t, e, cp.Token, "1.3.0", ""); o != nil {
		t.Fatalf("eski ajana teklif gitmemeli: %+v", o)
	}
	if st := probeUpdateState(t, e, id); st.State != agentupdate.StateUnsupported {
		t.Fatalf("eski ajan unsupported olmalı: %+v", st)
	}

	// Güncellemeyi destekleyen ajan: imzalı teklif gelir, imza ve özet doğrulanır.
	o := offerOf(t, e, cp.Token, "1.3.0", plat)
	if o == nil || o.Version != "1.3.1" || o.OS != tuOS || o.Arch != tuArch {
		t.Fatalf("teklif bekleniyordu: %+v", o)
	}
	pub, _ := agentupdate.PublicKey()
	if err := o.Signed.Verify(pub); err != nil {
		t.Fatalf("teklifin imzası doğrulanmalı: %v", err)
	}
	code, got := agentCall(t, e, "GET", o.URL, cp.Token, "1.3.0", plat, nil)
	if h := sha256.Sum256(got); code != 200 || hex.EncodeToString(h[:]) != o.SHA256 || !bytes.Equal(got, bin) {
		t.Fatalf("indirilen program teklifle uyuşmalı: %d %q", code, got)
	}
	if st := probeUpdateState(t, e, id); st.State != agentupdate.StateOutdated || !st.AutoEffective {
		t.Fatalf("outdated + otomatik bekleniyordu: %+v", st)
	}

	// Genel ayar kapatılınca teklif gitmez (kısmi gövde diğer ayarları bozmaz).
	e.mustDo("PUT", "/api/settings", map[string]any{"backup_keep": 5, "agent_auto_update": false}, nil, 200)
	var saved store.AppSettings
	e.mustDo("GET", "/api/settings", nil, &saved, 200)
	if saved.AgentAutoUpdateOn() || saved.BackupKeep != 5 || saved.RetentionRawDays != 14 || len(saved.CertDays) == 0 {
		t.Fatalf("kısmi ayar gövdesi yalnızca verilen alanları değiştirmeli: %+v", saved)
	}
	if o := offerOf(t, e, cp.Token, "1.3.0", plat); o != nil {
		t.Fatal("otomatik güncelleme kapalıyken teklif gitmemeli")
	}
	// Ajan başına açık: genel ayarı geçersiz kılar.
	e.mustDo("PUT", fmt.Sprintf("/api/probes/%d", id), map[string]any{"auto_update": true}, nil, 200)
	if o := offerOf(t, e, cp.Token, "1.3.0", plat); o == nil {
		t.Fatal("ajan başına açıkken teklif gitmeli")
	}
	e.mustDo("PUT", fmt.Sprintf("/api/probes/%d", id), map[string]any{"auto_update": nil}, nil, 200)
	if st := probeUpdateState(t, e, id); st.Auto != nil || st.AutoEffective {
		t.Fatalf("genel ayara dönmeli: %+v", st)
	}
	if o := offerOf(t, e, cp.Token, "1.3.0", plat); o != nil {
		t.Fatal("genel ayar kapalıyken teklif gitmemeli")
	}

	// "Şimdi güncelle": ayar kapalı olsa da bir kez teklif edilir.
	e.mustDo("POST", fmt.Sprintf("/api/probes/%d/update", id), nil, nil, 200)
	if o := offerOf(t, e, cp.Token, "1.3.0", plat); o == nil {
		t.Fatal("istenen güncelleme teklif edilmeli")
	}
	// Ajan başladığını bildirir → güncelleniyor; yeni sürümle gelince güncel.
	if code, body := agentCall(t, e, "POST", "/api/probe/update", cp.Token, "1.3.0", plat,
		agentupdate.Report{Status: agentupdate.StatusStarted, Version: "1.3.1"}); code != 204 {
		t.Fatalf("durum raporu: %d %s", code, body)
	}
	if st := probeUpdateState(t, e, id); st.State != agentupdate.StateUpdating {
		t.Fatalf("updating bekleniyordu: %+v", st)
	}
	if o := offerOf(t, e, cp.Token, "1.3.1", plat); o != nil {
		t.Fatal("güncel ajana teklif gitmemeli")
	}
	st := probeUpdateState(t, e, id)
	if st.State != agentupdate.StateCurrent || st.Requested {
		t.Fatalf("güncel olmalı ve istek temizlenmeli: %+v", st)
	}
	p, err := f.st.GetProbe(context.Background(), id)
	if err != nil || p.UpdateStatus != "" || p.UpdateRequestedAt != 0 || p.Version != "1.3.1" {
		t.Fatalf("kayıt temizlenmeli: %+v %v", p.ProbeUpdate, err)
	}
	audit, _ := f.st.ListAudit(context.Background(), 0, 50)
	var found bool
	for _, a := range audit {
		if a.Action == "agent.update" && a.Detail == "1.3.0 → 1.3.1 (otomatik güncelleme)" {
			found = true
		}
	}
	if !found {
		t.Fatalf("işlem kaydına güncelleme yazılmalı: %+v", audit)
	}
	// Güncel ajan için "Şimdi güncelle" reddedilir.
	if code := e.do("POST", fmt.Sprintf("/api/probes/%d/update", id), nil, nil); code != 400 {
		t.Fatalf("güncel ajana istek 400 olmalı: %d", code)
	}
}

func TestAgentUpdateFailureAndUpdateAll(t *testing.T) {
	f, _ := agentUpdateEnv(t, "1.3.1")
	e := f.env
	plat := tuOS + "/" + tuArch
	var a, b serverSetup
	e.mustDo("POST", "/api/servers", map[string]any{"name": "A"}, &a, 201)
	e.mustDo("POST", "/api/servers", map[string]any{"name": "B"}, &b, 201)
	offerOf(t, e, b.Token, "1.3.0", plat+";off") // B: ajan tarafında kapalı

	if o := offerOf(t, e, a.Token, "1.3.0", plat); o == nil {
		t.Fatal("teklif bekleniyordu")
	}
	if code, body := agentCall(t, e, "POST", "/api/probe/update", a.Token, "1.3.0", plat,
		agentupdate.Report{Status: agentupdate.StatusFailed, Version: "1.3.1", Error: "SHA-256 uyuşmuyor"}); code != 204 {
		t.Fatalf("durum raporu: %d %s", code, body)
	}
	st := probeUpdateState(t, e, a.Probe.ID)
	if st.State != agentupdate.StateFailed || st.Note != "SHA-256 uyuşmuyor" {
		t.Fatalf("failed + neden bekleniyordu: %+v", st)
	}
	// Başarısız ajana kendiliğinden yeniden teklif gitmez (döngü olmasın).
	if o := offerOf(t, e, a.Token, "1.3.0", plat); o != nil {
		t.Fatal("başarısızlıktan hemen sonra teklif gitmemeli")
	}
	if st := probeUpdateState(t, e, b.Probe.ID); st.State != agentupdate.StateOff {
		t.Fatalf("B off olmalı: %+v", st)
	}
	// Tümünü güncelle: yalnızca güncellenebilen (A) istenir; B ajan tarafında kapalı.
	var res struct {
		Requested int `json:"requested"`
	}
	e.mustDo("POST", "/api/probes/update-all?kind=server", nil, &res, 200)
	if res.Requested != 1 {
		t.Fatalf("1 ajan istenmeli: %+v", res)
	}
	if o := offerOf(t, e, a.Token, "1.3.0", plat); o == nil {
		t.Fatal("istekten sonra yeniden teklif edilmeli")
	}
	if o := offerOf(t, e, b.Token, "1.3.0", plat+";off"); o != nil {
		t.Fatal("ajan tarafında kapalıysa teklif gitmemeli")
	}

	// İmza dosyası yoksa (imzasız derleme) teklif yok, durum unsigned.
	os.Remove(filepath.Join(f.s.AgentDir, "uptime-"+tuOS+"-"+tuArch+".sig"))
	if o := offerOf(t, e, a.Token, "1.3.0", plat); o != nil {
		t.Fatal("imzasız derlemede teklif gitmemeli")
	}
	if st := probeUpdateState(t, e, a.Probe.ID); st.State != agentupdate.StateUnsigned {
		t.Fatalf("unsigned bekleniyordu: %+v", st)
	}
	// Bozuk platform başlığı yok sayılır (kayıtlı platform korunur).
	agentCall(t, e, "GET", "/api/probe/jobs", a.Token, "1.3.0", "linux/amd64;rm -rf", nil)
	if p, _ := f.st.GetProbe(context.Background(), a.Probe.ID); p.Platform != plat {
		t.Fatalf("geçersiz platform başlığı yazılmamalı: %q", p.Platform)
	}
}

func TestAgentUpdateSignatureMismatch(t *testing.T) {
	plat := tuOS + "/" + tuArch
	t.Run("program değişti", func(t *testing.T) {
		f, _ := agentUpdateEnv(t, "1.3.1")
		var cp serverSetup
		f.env.mustDo("POST", "/api/servers", map[string]any{"name": "S"}, &cp, 201)
		if err := os.WriteFile(filepath.Join(f.s.AgentDir, "uptime-"+tuOS+"-"+tuArch), []byte("baska-program"), 0o755); err != nil {
			t.Fatal(err)
		}
		if o := offerOf(t, f.env, cp.Token, "1.3.0", plat); o != nil {
			t.Fatal("özet uyuşmuyorsa teklif gitmemeli")
		}
	})
	t.Run("başka anahtar", func(t *testing.T) {
		f, _ := agentUpdateEnv(t, "1.3.1")
		var cp serverSetup
		f.env.mustDo("POST", "/api/servers", map[string]any{"name": "S"}, &cp, 201)
		other, _, _ := ed25519.GenerateKey(rand.Reader)
		agentupdate.TestPublicKey = other
		if o := offerOf(t, f.env, cp.Token, "1.3.0", plat); o != nil {
			t.Fatal("başka anahtarla imzalıysa teklif gitmemeli")
		}
	})
}

func TestAgentUpdateUnversionedPanel(t *testing.T) {
	f, _ := agentUpdateEnv(t, "977e923")
	e := f.env
	var cp serverSetup
	e.mustDo("POST", "/api/servers", map[string]any{"name": "S"}, &cp, 201)
	if o := offerOf(t, e, cp.Token, "1.3.0", tuOS+"/"+tuArch); o != nil {
		t.Fatal("commit derlemesi panel teklif etmemeli")
	}
	if st := probeUpdateState(t, e, cp.Probe.ID); st.State != agentupdate.StateUnversioned {
		t.Fatalf("unversioned bekleniyordu: %+v", st)
	}
}
