package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/kadirsungurlu/bekci/internal/denetim"
	"github.com/kadirsungurlu/bekci/internal/eklenti"
	"github.com/kadirsungurlu/bekci/internal/eklentiajan"
	"github.com/kadirsungurlu/bekci/internal/servers"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// Denetim sonuçları (iş 741, docs/GUVENLIK-EKLENTI-730.md §B.4). Ücretsiz:
// eklenti (739) ve yerleşik denetimler (743) aynı uçları kullanır.
func init() {
	RegisterRoutes(func(s *Server, mux *http.ServeMux) {
		mux.Handle("POST /api/probe/checks", s.probeOnly(s.probeChecks))
		mux.Handle("GET /api/servers/{id}/checks", s.auth(s.serversOnly(s.listChecks)))
		mux.Handle("GET /api/servers/{id}/checks/{name}/series", s.auth(s.serversOnly(s.checkSeries)))
		mux.Handle("PUT /api/servers/{id}/checks/{name}", s.admin(s.putCheck))
		mux.Handle("GET /api/check-kinds", s.auth(s.checkKinds))
	})
}

const (
	maxCheckBatch     = 20      // B.4: istek başına sonuç
	maxCheckBodyBytes = 1 << 20 // 20 sonuç x (50 metrik + ileti + 1 KiB stderr)
	maxCheckRules     = 11      // durum + 10 metrik kuralı
)

func (s *Server) probeChecks(w http.ResponseWriter, r *http.Request) {
	p := probeFrom(r)
	var in struct {
		Results []servers.CheckReport `json:"results"`
	}
	body := http.MaxBytesReader(w, r.Body, maxCheckBodyBytes)
	if err := json.NewDecoder(body).Decode(&in); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "İstek gövdesi çok büyük")
			return
		}
		writeError(w, http.StatusBadRequest, "Geçersiz istek gövdesi: "+err.Error())
		return
	}
	if len(in.Results) > maxCheckBatch {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("İstek başına en fazla %d sonuç gönderilebilir", maxCheckBatch))
		return
	}
	if err := s.servers.IngestChecks(r.Context(), p, in.Results); err != nil {
		s.dbError(w, err)
		return
	}
	if len(in.Results) > 0 && p.Kind == store.ProbeKindServer {
		s.servers.Publish(r.Context(), p.ID)
	}
	w.WriteHeader(http.StatusNoContent)
}

// checksFor iş listesine giden denetimler (yalnız sunucu ajanı; boş liste =
// hepsi kapalı, bu yüzden okuma hatasında alan hiç yazılmaz -> istek 500).
func (s *Server) checksFor(r *http.Request, p store.Probe) ([]store.CheckJob, error) {
	if p.Kind != store.ProbeKindServer || !p.Active {
		return []store.CheckJob{}, nil
	}
	jobs, err := s.store.CheckJobs(r.Context(), p.ID)
	if err != nil {
		return nil, err
	}
	// Yerleşik: panelin tanımadığı (Pro modülü yok) ya da lisansı kapalı
	// türler gitmez; ajan öğe listeden çıkınca durdurur.
	out := jobs[:0]
	for _, j := range jobs {
		if j.Source == "builtin" {
			if t, ok := denetim.Bul(j.Kind); !ok || !s.denetimAcik(t) {
				continue
			}
		}
		out = append(out, j)
	}
	return out, nil
}

func (s *Server) denetimAcik(t denetim.Tur) bool { return !t.Pro() || s.proAcik(t.Ozellik) }

// checkKindView GET /api/check-kinds öğesi (form şeması, 743).
type checkKindView struct {
	denetim.Tur
	Pro    bool `json:"pro"`
	Locked bool `json:"locked"`
}

func (s *Server) checkKinds(w http.ResponseWriter, r *http.Request) {
	out := []checkKindView{}
	for _, t := range denetim.Turler() {
		out = append(out, checkKindView{Tur: t, Pro: t.Pro(), Locked: !s.denetimAcik(t)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"kinds": out})
}

func (s *Server) listChecks(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if _, err := s.serverProbe(r, id); err != nil {
		s.dbError(w, err)
		return
	}
	list, err := s.store.ListChecks(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	rules, err := s.store.CheckAlerts(r.Context(), id, "")
	if err != nil {
		s.dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"checks": list, "rules": rules, "max": store.MaxChecksPerServer,
		"samples": s.store.DBMode() != store.DBModeSQLite, "now": s.now().Unix()})
}

// checkRanges seri pencereleri (sn); 2 günden uzunu saatlik özetten.
var checkRanges = map[string]int64{"1h": 3600, "6h": 6 * 3600, "24h": 86400, "2d": 2 * 86400, "7d": 7 * 86400,
	"30d": 30 * 86400, "90d": 90 * 86400, "365d": 365 * 86400}

func (s *Server) checkSeries(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	name, metric := r.PathValue("name"), r.URL.Query().Get("metric")
	if !servers.ValidCheckName(name) || !eklenti.ValidName(metric) {
		writeError(w, http.StatusBadRequest, "Geçersiz denetim ya da metrik adı")
		return
	}
	rng := r.URL.Query().Get("range")
	if rng == "" {
		rng = "24h"
	}
	span, ok := checkRanges[rng]
	if !ok {
		writeError(w, http.StatusBadRequest, "Geçersiz aralık")
		return
	}
	if _, err := s.serverProbe(r, id); err != nil {
		s.dbError(w, err)
		return
	}
	c, err := s.store.GetCheck(r.Context(), id, name)
	if err != nil {
		s.dbError(w, err)
		return
	}
	now := s.now().Unix()
	hourly := span > 2*86400
	pts, err := s.store.CheckSeries(r.Context(), id, name, metric, now-span, now+1, hourly)
	if err != nil {
		s.dbError(w, err)
		return
	}
	var unit string
	var ms []eklenti.Metric
	if json.Unmarshal(c.Metrics, &ms) == nil {
		for _, m := range ms {
			if m.Name == metric {
				unit = m.Unit
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"metric": metric, "unit": unit, "range": rng, "hourly": hourly, "points": pts})
}

// checkRuleInput denetim kuralı: metric boş = durum kuralı (threshold bit
// maskesi: 1 warning, 2 critical, 4 unknown), dolu = metrik eşik kuralı.
type checkRuleInput struct {
	Metric        string  `json:"metric"`
	Threshold     float64 `json:"threshold"`
	WarnThreshold float64 `json:"warn_threshold"`
	Minutes       int     `json:"minutes"`
	Below         bool    `json:"below"`
	Active        *bool   `json:"active"`
}

type checkInput struct {
	Enabled  *bool            `json:"enabled"`
	Interval *int             `json:"interval"`
	Manifest json.RawMessage  `json:"manifest"` // imzalı zarf; null değişmez, "" kaldırır
	Body     *string          `json:"body"`     // base64 gövde (manifestle birlikte)
	Rules    []checkRuleInput `json:"rules"`    // nil: kurallar değişmez
	Kind     *string          `json:"kind"`     // yerleşik denetim türü (yalnız oluştururken)
	Params   json.RawMessage  `json:"params"`   // yerleşik denetim parametreleri
}

func validateCheckRules(name string, in []checkRuleInput) ([]store.ServerAlert, error) {
	if len(in) > maxCheckRules {
		return nil, fmt.Errorf("Bir denetimde en fazla %d kural olabilir", maxCheckRules)
	}
	seen := map[string]bool{}
	out := make([]store.ServerAlert, 0, len(in))
	for _, a := range in {
		a.Metric = strings.TrimSpace(a.Metric)
		mount := name
		if a.Metric != "" {
			if !eklenti.ValidName(a.Metric) {
				return nil, errors.New("Metrik adı harf, rakam, _ . - içermeli (en çok 64)")
			}
			mount = name + "/" + a.Metric
		}
		if seen[mount] {
			return nil, errors.New("Aynı metrik için birden fazla kural olamaz")
		}
		seen[mount] = true
		if a.Minutes < 1 || a.Minutes > 1440 {
			return nil, errors.New("Süre 1-1440 dakika olmalı")
		}
		rule := store.ServerAlert{Metric: store.MetricCheck, Mount: mount, Minutes: a.Minutes, Active: a.Active == nil || *a.Active}
		if a.Metric == "" {
			mask := int(a.Threshold)
			if float64(mask) != a.Threshold || mask < 1 || mask > 7 {
				return nil, errors.New("Durum kuralı en az bir durum seçmeli")
			}
			rule.Threshold = a.Threshold
		} else {
			if math.IsNaN(a.Threshold) || math.IsInf(a.Threshold, 0) || math.IsNaN(a.WarnThreshold) || math.IsInf(a.WarnThreshold, 0) {
				return nil, errors.New("Eşik sayı olmalı")
			}
			if a.WarnThreshold != 0 && ((!a.Below && a.WarnThreshold >= a.Threshold) || (a.Below && a.WarnThreshold <= a.Threshold)) {
				return nil, errors.New("Uyarı eşiği kritik eşikten önce gelmeli")
			}
			rule.Threshold, rule.WarnThreshold, rule.Below = a.Threshold, a.WarnThreshold, a.Below
		}
		out = append(out, rule)
	}
	return out, nil
}

func (s *Server) putCheck(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	name := r.PathValue("name")
	if !servers.ValidCheckName(name) {
		writeError(w, http.StatusBadRequest, "Denetim adı küçük harf, rakam ve - içermeli")
		return
	}
	p, err := s.serverProbe(r, id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	var in checkInput
	if !readJSON(w, r, &in) {
		return
	}
	cfg := store.CheckConfig{Enabled: in.Enabled}
	var detail []string
	// Yerleşik denetim (743): tür kaydından şema, aralık ve lisans.
	var tur *denetim.Tur
	cur, err := s.store.GetCheck(r.Context(), id, name)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		s.dbError(w, err)
		return
	}
	switch {
	case exists && cur.Source == "builtin":
		if in.Kind != nil && *in.Kind != cur.Kind {
			writeError(w, http.StatusBadRequest, "Denetim türü değiştirilemez; yeni ad ile ekleyin")
			return
		}
		t, ok := denetim.Bul(cur.Kind)
		if !ok {
			writeError(w, http.StatusBadRequest, "Bu panel denetim türünü tanımıyor: "+cur.Kind)
			return
		}
		tur = &t
	case in.Kind != nil && exists:
		writeError(w, http.StatusBadRequest, "Bu ad bir eklentiye ait; yerleşik denetim için başka ad seçin")
		return
	case in.Kind != nil:
		t, ok := denetim.Bul(*in.Kind)
		if !ok {
			writeError(w, http.StatusBadRequest, "Bilinmeyen denetim türü")
			return
		}
		tur = &t
		cfg.Source, cfg.Kind = "builtin", t.Kind
		detail = append(detail, "tür "+t.Kind)
	case len(in.Params) > 0 && string(in.Params) != "null":
		writeError(w, http.StatusBadRequest, "Parametreler yalnız yerleşik denetimde kullanılır")
		return
	}
	if tur != nil {
		if !s.denetimAcik(*tur) && (in.Enabled == nil || *in.Enabled) {
			writeJSON(w, http.StatusPaymentRequired, map[string]string{
				"error": "Bu denetim türü için Pro lisansı gerekli", "code": "pro_lisans"})
			return
		}
		if len(in.Manifest) > 0 && string(in.Manifest) != "null" || in.Body != nil {
			writeError(w, http.StatusBadRequest, "Yerleşik denetime manifest yüklenmez")
			return
		}
		if (len(in.Params) > 0 && string(in.Params) != "null") || !exists {
			params, err := tur.Dogrula(in.Params)
			if err != nil {
				writeError(w, http.StatusBadRequest, "Parametre hatası: "+err.Error())
				return
			}
			b, _ := json.Marshal(params)
			ps := string(b)
			cfg.Params = &ps
			detail = append(detail, "parametreler")
		}
		if in.Interval != nil {
			if *in.Interval < tur.Interval.Min || *in.Interval > tur.Interval.Max {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("Aralık %d-%d saniye olmalı", tur.Interval.Min, tur.Interval.Max))
				return
			}
			cfg.Interval = in.Interval
			detail = append(detail, fmt.Sprintf("aralık %d sn", *in.Interval))
		} else if !exists {
			d := tur.Interval.Default
			cfg.Interval = &d
		}
		if !exists && in.Rules == nil && len(tur.Kurallar) > 0 {
			// Türün varsayılan kuralları + kritik durum kuralı (EnsureCheckStatusRule ile aynı).
			in.Rules = []checkRuleInput{{Threshold: store.CheckStatusCritical, Minutes: 1}}
			for _, k := range tur.Kurallar {
				in.Rules = append(in.Rules, checkRuleInput{Metric: k.Metric, Threshold: k.Threshold, Minutes: k.Minutes, Below: k.Below, Active: new(!k.Pasif)})
			}
		}
		s.saveCheck(w, r, p, id, name, cfg, in, detail)
		return
	}
	// Manifest: kayıtlı ya da yeni. İmza ajanda doğrulanır; burada biçim, ad,
	// gövde özeti ve aralık sınırları erken denetlenir.
	var man *eklentiajan.Manifest
	if len(in.Manifest) > 0 && string(in.Manifest) != "null" {
		if string(in.Manifest) == `""` {
			empty := ""
			cfg.Manifest, cfg.Body = &empty, &empty
			detail = append(detail, "manifest -")
		} else {
			m, err := eklentiajan.InspectEnvelope(in.Manifest)
			if err != nil {
				writeError(w, http.StatusBadRequest, "Manifest geçersiz: "+err.Error())
				return
			}
			if m.ID != name {
				writeError(w, http.StatusBadRequest, "Manifest kimliği denetim adıyla aynı olmalı")
				return
			}
			if in.Body == nil {
				writeError(w, http.StatusBadRequest, "Manifestle birlikte gövde (base64) gönderilmeli")
				return
			}
			b, err := base64.StdEncoding.Strict().DecodeString(*in.Body)
			if err != nil || len(b) == 0 || len(b) > eklentiajan.MaxBody {
				writeError(w, http.StatusBadRequest, "Gövde geçersiz ya da çok büyük")
				return
			}
			if sum := sha256.Sum256(b); hex.EncodeToString(sum[:]) != m.SHA256 {
				writeError(w, http.StatusBadRequest, "Gövde manifestteki sha256 ile uyuşmuyor")
				return
			}
			raw := string(in.Manifest)
			cfg.Manifest, cfg.Body, cfg.Version = &raw, in.Body, &m.Version
			man = m
			detail = append(detail, fmt.Sprintf("manifest v%d", m.Version))
		}
	} else if in.Body != nil {
		writeError(w, http.StatusBadRequest, "Gövde yalnız manifestle birlikte gönderilebilir")
		return
	}
	if man == nil && cfg.Manifest == nil {
		raw, err := s.store.CheckManifest(r.Context(), id, name)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			s.dbError(w, err)
			return
		}
		if raw != "" {
			man, _ = eklentiajan.InspectEnvelope([]byte(raw))
		}
	}
	lo, hi := eklentiajan.MinInterval, eklentiajan.MaxInterval
	if man != nil {
		lo, hi = man.IntervalMin, man.IntervalMax
	}
	if in.Interval != nil {
		if *in.Interval < lo || *in.Interval > hi {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Aralık %d-%d saniye olmalı", lo, hi))
			return
		}
		cfg.Interval = in.Interval
		detail = append(detail, fmt.Sprintf("aralık %d sn", *in.Interval))
	} else if man != nil && cfg.Manifest != nil {
		// Yeni manifest: kayıtlı aralık sınır dışındaysa en küçüğe çekilir.
		cur := 0
		if c, err := s.store.GetCheck(r.Context(), id, name); err == nil {
			cur = c.Interval
		}
		if cur < lo || cur > hi {
			cfg.Interval = &lo
		}
	}
	s.saveCheck(w, r, p, id, name, cfg, in, detail)
}

// saveCheck yapılandırmayı ve kuralları yazar, işlem kaydı ve yayın yapar.
func (s *Server) saveCheck(w http.ResponseWriter, r *http.Request, p store.Probe, id int64, name string,
	cfg store.CheckConfig, in checkInput, detail []string) {
	var err error
	var rules []store.ServerAlert
	if in.Rules != nil {
		if rules, err = validateCheckRules(name, in.Rules); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		detail = append(detail, fmt.Sprintf("%d kural", len(rules)))
	}
	if in.Enabled != nil {
		detail = append(detail, map[bool]string{true: "açık", false: "kapalı"}[*in.Enabled])
	}
	now := s.now().Unix()
	created, err := s.store.SetCheckConfig(r.Context(), id, name, cfg, now)
	if errors.Is(err, store.ErrCheckLimit) {
		writeError(w, http.StatusConflict, fmt.Sprintf("Sunucu başına en fazla %d denetim olabilir", store.MaxChecksPerServer))
		return
	}
	if err != nil {
		s.dbError(w, err)
		return
	}
	if created && in.Rules == nil {
		if err := s.store.EnsureCheckStatusRule(r.Context(), id, name); err != nil {
			s.dbError(w, err)
			return
		}
	}
	if in.Rules != nil {
		if _, err := s.store.ReplaceCheckAlerts(r.Context(), id, name, rules, now); err != nil {
			s.dbError(w, err)
			return
		}
	}
	c, err := s.store.GetCheck(r.Context(), id, name)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if !c.Enabled {
		if err := s.servers.ResolveCheck(r.Context(), p, name); err != nil {
			s.dbError(w, err)
			return
		}
	}
	saved, err := s.store.CheckAlerts(r.Context(), id, name)
	if err != nil {
		s.dbError(w, err)
		return
	}
	s.audit(r, store.User{}, "server.check", "server", id, p.Name, name+": "+strings.Join(detail, ", "))
	s.engine.JobsChanged() // ajan uzun yoklamadan hemen döner, yeni listeyi alır
	s.servers.Publish(r.Context(), id)
	writeJSON(w, http.StatusOK, map[string]any{"check": c, "rules": saved})
}
