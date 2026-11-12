package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/kadirsungurlu/bekci/internal/denetim"
	"github.com/kadirsungurlu/bekci/internal/eklenti"
	"github.com/kadirsungurlu/bekci/internal/eklentiajan"
	"github.com/kadirsungurlu/bekci/internal/store"
)

func init() {
	denetim.Kaydet(denetim.Tur{Kind: "protest", Ozellik: "hosting", Interval: denetim.Aralik{Min: 60, Max: 3600, Default: 120},
		Calistir: func(context.Context, denetim.Girdi) eklenti.Result { return eklenti.Result{Status: eklenti.OK} }})
	denetim.Kaydet(denetim.Tur{Kind: "kuralli", Interval: denetim.Aralik{Min: 60, Max: 3600, Default: 120},
		Kurallar: []denetim.Kural{{Metric: "total", Threshold: 500, Minutes: 15}, {Metric: "free", Threshold: 5, Minutes: 5, Below: true}, {Metric: "slow", Threshold: 10, Minutes: 5, Pasif: true}},
		Calistir: func(context.Context, denetim.Girdi) eklenti.Result { return eklenti.Result{Status: eklenti.OK} }})
}

// Türün varsayılan kuralları (744): oluştururken kural verilmezse durum
// kuralı + türün eşikleri kurulur; kural verilirse onlar esas.
func TestChecksBuiltinDefaultRules(t *testing.T) {
	f := newServersEnv(t)
	var sv serverSetup
	f.env.mustDo("POST", "/api/servers", map[string]any{"name": "mail-1"}, &sv, 201)
	base := "/api/servers/" + itoa(sv.Probe.ID) + "/checks"
	var out struct {
		Rules []store.ServerAlert `json:"rules"`
	}
	f.env.mustDo("PUT", base+"/k1", map[string]any{"kind": "kuralli"}, &out, 200)
	got := map[string]store.ServerAlert{}
	for _, r := range out.Rules {
		got[r.Mount] = r
	}
	if len(got) != 4 || got["k1/slow"].Active || got["k1"].Threshold != store.CheckStatusCritical || got["k1/total"].Threshold != 500 ||
		got["k1/total"].Minutes != 15 || !got["k1/free"].Below || !got["k1/total"].Active {
		t.Fatalf("varsayılan kurallar: %+v", out.Rules)
	}
	f.env.mustDo("PUT", base+"/k2", map[string]any{"kind": "kuralli", "rules": []map[string]any{{"threshold": 6, "minutes": 2}}}, &out, 200)
	if len(out.Rules) != 1 || out.Rules[0].Mount != "k2" {
		t.Fatalf("verilen kurallar: %+v", out.Rules)
	}
	// Var olan denetimi güncellemek kuralları yeniden kurmaz.
	f.env.mustDo("PUT", base+"/k2", map[string]any{"interval": 300}, &out, 200)
	if len(out.Rules) != 1 {
		t.Fatalf("güncelleme: %+v", out.Rules)
	}
}

// Yerleşik denetim (743): tür listesi, form doğrulaması, jobs, ajan
// çalıştırıcıdan dönen sonuç ve Pro kilidi.
func TestChecksBuiltinAPI(t *testing.T) {
	f := newServersEnv(t)
	admin := f.env
	var sv serverSetup
	admin.mustDo("POST", "/api/servers", map[string]any{"name": "web-1"}, &sv, 201)
	base := "/api/servers/" + itoa(sv.Probe.ID) + "/checks"

	var kinds struct {
		Kinds []struct {
			Kind   string          `json:"kind"`
			Pro    bool            `json:"pro"`
			Locked bool            `json:"locked"`
			Params []denetim.Param `json:"params"`
			Label  denetim.Metin   `json:"label"`
		} `json:"kinds"`
	}
	admin.mustDo("GET", "/api/check-kinds", nil, &kinds, 200)
	got := map[string]bool{}
	for _, k := range kinds.Kinds {
		got[k.Kind] = k.Locked
		if k.Kind == "noop" && (k.Pro || len(k.Params) != 5 || k.Label.EN == "") {
			t.Fatalf("noop: %+v", k)
		}
	}
	if locked, ok := got["noop"]; !ok || locked || !got["protest"] {
		t.Fatalf("türler: %v", got)
	}

	// Doğrulama: bilinmeyen tür, loopback dışı hedef, aralık, manifest.
	bad := []map[string]any{
		{"kind": "yok"},
		{"kind": "noop", "params": map[string]any{"target": "10.1.2.3:3306"}},
		{"kind": "noop", "params": map[string]any{"status": "bad"}},
		{"kind": "noop", "interval": 10},
		{"kind": "noop", "manifest": json.RawMessage(`{"a":1}`), "body": "eA=="},
	}
	for _, in := range bad {
		if st := admin.do("PUT", base+"/n1", in, nil); st != http.StatusBadRequest {
			t.Fatalf("%v: %d", in, st)
		}
	}
	// Pro türü lisanssız 402.
	if st := admin.do("PUT", base+"/p1", map[string]any{"kind": "protest"}, nil); st != http.StatusPaymentRequired {
		t.Fatalf("pro: %d", st)
	}

	var out struct {
		Check store.CheckState    `json:"check"`
		Rules []store.ServerAlert `json:"rules"`
	}
	admin.mustDo("PUT", base+"/n1", map[string]any{"kind": "noop", "params": map[string]any{"status": "critical", "credential": "db",
		"target": "10.1.2.3:3306"}}, &out, 200)
	if out.Check.Source != "builtin" || out.Check.Kind != "noop" || out.Check.Interval != 300 || len(out.Rules) != 1 ||
		!strings.Contains(string(out.Check.Params), `"credential":"db"`) || !strings.Contains(string(out.Check.Params), `"value":1`) {
		t.Fatalf("oluşturma: %+v", out)
	}
	// Tür değişmez, eklenti adına kind verilemez, aralık türün sınırında.
	if st := admin.do("PUT", base+"/n1", map[string]any{"kind": "protest"}, nil); st != 400 {
		t.Fatalf("tür değişimi: %d", st)
	}
	if st := admin.do("PUT", base+"/n1", map[string]any{"interval": 90000}, nil); st != 400 {
		t.Fatalf("aralık: %d", st)
	}
	admin.mustDo("PUT", base+"/n1", map[string]any{"params": map[string]any{"status": "warning"}, "interval": 60}, &out, 200)
	if strings.Contains(string(out.Check.Params), "credential") || out.Check.Interval != 60 {
		t.Fatalf("güncelleme: %+v", out.Check)
	}

	jobs := func() []eklentiajan.Item {
		st, _, b := admin.rawReq("GET", "/api/probe/jobs", bearer(sv.Token), nil)
		var r struct {
			Checks []eklentiajan.Item `json:"checks"`
		}
		if st != 200 || json.Unmarshal(b, &r) != nil {
			t.Fatalf("jobs: %d %s", st, b)
		}
		return r.Checks
	}
	j := jobs()
	if len(j) != 1 || j[0].Source != "builtin" || j[0].Kind != "noop" || j[0].Interval != 60 || !strings.Contains(string(j[0].Params), "warning") {
		t.Fatalf("jobs: %+v", j)
	}

	// Ajan çalıştırıcıdan geçen sonuç panele (uçtan uca, yerel süreç).
	s := &eklentiajan.Scheduler{Paths: eklentiajan.Paths{Kimlik: t.TempDir()}}
	res := s.RunOne(context.Background(), j[0])
	if st, _, b := admin.rawReq("POST", "/api/probe/checks", bearer(sv.Token), map[string]any{"results": []any{res}}); st != http.StatusNoContent {
		t.Fatalf("sonuç: %d %s", st, b)
	}
	var list struct {
		Checks []store.CheckState `json:"checks"`
	}
	admin.mustDo("GET", base, nil, &list, 200)
	if len(list.Checks) != 1 || list.Checks[0].Status != "warning" || list.Checks[0].Source != "builtin" || list.Checks[0].Kind != "noop" {
		t.Fatalf("liste: %+v", list.Checks)
	}

	// Pro türü: lisans açıkken kaydedilir, kapanınca işten düşer.
	acik := true
	f.s.SetProOzellik(func(o string) bool { return acik && o == "hosting" })
	admin.mustDo("PUT", base+"/p1", map[string]any{"kind": "protest"}, &out, 200)
	if len(jobs()) != 2 {
		t.Fatal("pro işi yok")
	}
	acik = false
	if j := jobs(); len(j) != 1 || j[0].Name != "n1" {
		t.Fatalf("lisanssız pro işi: %+v", j)
	}
	if st := admin.do("PUT", base+"/p1", map[string]any{"interval": 300}, nil); st != http.StatusPaymentRequired {
		t.Fatalf("lisanssız düzenleme: %d", st)
	}
	admin.mustDo("PUT", base+"/p1", map[string]any{"enabled": false}, &out, 200) // kapatmak serbest
}
