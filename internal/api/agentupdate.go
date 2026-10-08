package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kadirsungurlu/bekci/internal/agentupdate"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// Ajanların imzalı kendini güncellemesi — panel tarafı (bkz. internal/agentupdate).
//
// Panel, iş listesi yanıtına (GET /api/probe/jobs) "update" alanı ekler: kendi
// sürümü ajanın sürümünden yeniyse, ajanın platformu için imzalı bir derleme
// varsa ve güncelleme bu ajan için açıksa (genel ayar + ajan başına geçersiz
// kılma) ya da panelden istenmişse. Ajan teklifi gömülü açık anahtarla
// doğrular; panelin kendisi de imzayı doğrular ki yanlış anahtarla/sürümle
// imzalanmış bir dosya yanlışlıkla sunulmasın. İmza dosyaları derlemede
// AgentDir'e yazılır: uptime-<os>-<arch>.sig (cmd/ajanimza). İmzasız derleme
// (anahtar olmadan derlenen panel) hiçbir ajana güncelleme sunmaz.
//
// Ajan durumunu POST /api/probe/update ile bildirir (started | failed |
// rolled_back); yeni sürümle geri gelince (X-Probe-Version değişti) güncelleme
// tamamlanmış sayılır ve işlem kaydına yazılır.

const (
	// agentUpdateStale "güncelleniyor" durumunda bu kadar süre yeni sürüm
	// gelmezse başarısız sayılır (ajan indirmeden sonra geri dönmedi).
	agentUpdateStale = 10 * time.Minute
	// agentUpdateRetry başarısız güncelleme bu kadar sonra kendiliğinden
	// yeniden teklif edilir (geçici ağ hatası vb.); daha erken için "Şimdi güncelle".
	agentUpdateRetry = 6 * time.Hour
	// agentUpdateNoteMax ajanın bildirdiği hata metninin saklanan en uzun hali.
	agentUpdateNoteMax = 300
)

func init() {
	RegisterRoutes(func(s *Server, mux *http.ServeMux) {
		mux.Handle("POST /api/probes/{id}/update", s.admin(s.requestProbeUpdate))
		mux.Handle("POST /api/probes/update-all", s.admin(s.requestAllProbeUpdates))
		mux.Handle("POST /api/probe/update", s.probeOnly(s.probeUpdateReport))
	})
}

// İmza dosyaları ------------------------------------------------------------------------

type sigEntry struct {
	signed  agentupdate.Signed
	ok      bool
	reason  string
	sigMod  int64
	sigSize int64
	binSHA  string
}

var (
	sigMu    sync.Mutex
	sigCache = map[string]sigEntry{}
	sigWarn  = map[string]string{} // yol → son loglanan neden (tekrar yazılmasın)
)

// agentSigned platformun ajan programı için panelin elindeki imzalı bildirim.
// ok=false ise reason açıklar (dosya yok, sürüm/özet uyuşmuyor, imza geçersiz).
// Sonuç imza dosyası ve program değişmediği sürece önbellekten gelir.
func (s *Server) agentSigned(goos, goarch string) (agentupdate.Signed, bool, string) {
	if s.AgentDir == "" {
		return agentupdate.Signed{}, false, "AGENT_DIR ayarlı değil"
	}
	path := filepath.Join(s.AgentDir, "uptime-"+goos+"-"+goarch+".sig")
	sum := s.agentBinarySHA256(goos, goarch)
	if sum == "" {
		return agentupdate.Signed{}, false, "bu platform için ajan programı yok"
	}
	st, err := os.Stat(path)
	if err != nil {
		return agentupdate.Signed{}, false, "imza dosyası yok (imzasız derleme)"
	}
	sigMu.Lock()
	e, cached := sigCache[path]
	sigMu.Unlock()
	if cached && e.sigMod == st.ModTime().Unix() && e.sigSize == st.Size() && e.binSHA == sum {
		return e.signed, e.ok, e.reason
	}
	e = sigEntry{sigMod: st.ModTime().Unix(), sigSize: st.Size(), binSHA: sum}
	e.signed, e.reason = s.loadSignature(path, goos, goarch, sum)
	e.ok = e.reason == ""
	sigMu.Lock()
	sigCache[path] = e
	warned := sigWarn[path]
	sigWarn[path] = e.reason
	sigMu.Unlock()
	if !e.ok && warned != e.reason {
		s.log.Warn("ajan imzası kullanılamıyor; bu platforma güncelleme sunulmayacak", "dosya", path, "neden", e.reason)
	}
	return e.signed, e.ok, e.reason
}

// loadSignature imza dosyasını okur ve bu panelin sürümü, platform ve
// programın özetiyle doğrular.
func (s *Server) loadSignature(path, goos, goarch, sum string) (agentupdate.Signed, string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return agentupdate.Signed{}, "imza dosyası okunamadı: " + err.Error()
	}
	var sg agentupdate.Signed
	if err := json.Unmarshal(b, &sg); err != nil {
		return agentupdate.Signed{}, "imza dosyası çözülemedi: " + err.Error()
	}
	switch {
	case sg.Version != s.version:
		return agentupdate.Signed{}, fmt.Sprintf("imza %s sürümü için, panel %s", sg.Version, s.version)
	case sg.OS != goos || sg.Arch != goarch:
		return agentupdate.Signed{}, fmt.Sprintf("imza %s/%s için", sg.OS, sg.Arch)
	case sg.SHA256 != sum:
		return agentupdate.Signed{}, "programın SHA-256'sı imzadakiyle uyuşmuyor"
	}
	pub, err := agentupdate.PublicKey()
	if err != nil {
		return agentupdate.Signed{}, "açık anahtar: " + err.Error()
	}
	if err := sg.Verify(pub); err != nil {
		return agentupdate.Signed{}, "imza geçersiz (başka anahtarla imzalanmış olabilir): " + err.Error()
	}
	return sg, ""
}

// Durum ----------------------------------------------------------------------------------

// agentPlatform "linux/amd64;off" değerini çözer: os, arch ve ajan tarafı
// kısıt ("" | "off" | "readonly").
func agentPlatform(p string) (goos, goarch, flag string) {
	plat, flag, _ := strings.Cut(p, ";")
	goos, goarch, _ = strings.Cut(plat, "/")
	return goos, goarch, flag
}

// agentUpdateStatus ajanın güncelleme durumu (arayüz rozeti ve teklif kararı).
func (s *Server) agentUpdateStatus(p store.Probe) *agentupdate.Status {
	st := &agentupdate.Status{Panel: s.version, Auto: p.AutoUpdate, Requested: p.UpdateRequestedAt > 0}
	st.AutoEffective = s.engine.Settings().AgentAutoUpdateOn()
	if p.AutoUpdate != nil {
		st.AutoEffective = *p.AutoUpdate
	}
	now := s.now()
	goos, goarch, flag := agentPlatform(p.Platform)
	switch {
	case p.Version == "":
		st.State = agentupdate.StateUnknown
	case p.Version == s.version:
		st.State = agentupdate.StateCurrent
	case p.Platform == "":
		// Sürüm farklı ama ajan platform bildirmiyor: bu özellikten önceki
		// sürüm; kendini güncelleyemez.
		st.State = agentupdate.StateUnsupported
	case p.UpdateStatus == store.UpdateStatusUpdating && now.Unix()-p.UpdateAt <= int64(agentUpdateStale/time.Second):
		st.State, st.At = agentupdate.StateUpdating, p.UpdateAt
	case p.UpdateStatus == store.UpdateStatusUpdating:
		st.State, st.At = agentupdate.StateFailed, p.UpdateAt
		st.Note = "ajan güncellemeden sonra yeni sürümle geri dönmedi"
	case p.UpdateStatus == store.UpdateStatusFailed && p.UpdateTarget == s.version && now.Unix()-p.UpdateAt <= int64(agentUpdateRetry/time.Second):
		st.State, st.At, st.Note = agentupdate.StateFailed, p.UpdateAt, p.UpdateNote
	case !agentupdate.IsVersion(s.version) || !agentupdate.IsVersion(p.Version):
		st.State = agentupdate.StateUnversioned
	case !agentupdate.Newer(p.Version, s.version):
		st.State = agentupdate.StateNewer
	case flag == "off":
		st.State = agentupdate.StateOff
	case flag == "readonly":
		st.State = agentupdate.StateReadOnly
	default:
		if _, ok, reason := s.agentSigned(goos, goarch); !ok {
			st.State, st.Note = agentupdate.StateUnsigned, reason
		} else {
			st.State = agentupdate.StateOutdated
		}
	}
	// Panel "güncelleniyor"/"başarısız" durumunu süre dolunca kendisi düşürür;
	// istek işareti de ancak teklif edilebilir durumda anlamlı.
	if st.State != agentupdate.StateOutdated && st.State != agentupdate.StateUpdating && st.State != agentupdate.StateFailed {
		st.Requested = false
	}
	return st
}

// updateOffer iş listesi yanıtına eklenecek teklif; yoksa nil.
func (s *Server) updateOffer(p store.Probe) *agentupdate.Offer {
	st := s.agentUpdateStatus(p)
	// "Güncelleniyor" iken de sunulur: ajan indirmeyi geçici bir nedenle (429)
	// erteleyip yeniden deneyebilsin; aynı sürümü tekrar tekrar indirmesini
	// ajanın kendi bekleme süresi engeller.
	offerable := st.Updatable() || st.State == agentupdate.StateUpdating
	if !offerable || !(st.AutoEffective || st.Requested) {
		return nil
	}
	goos, goarch, _ := agentPlatform(p.Platform)
	sg, ok, _ := s.agentSigned(goos, goarch)
	if !ok {
		return nil
	}
	return &agentupdate.Offer{Signed: sg, URL: "/api/probe/binary?os=" + goos + "&arch=" + goarch}
}

// agentVersionChanged ajan farklı bir sürümle bağlandı (probeOnly): bekleyen
// güncelleme durumu temizlenir, işlem kaydına yazılır.
func (s *Server) agentVersionChanged(ctx context.Context, p store.Probe, newVersion, ip string) {
	old := p.Version
	if old == "" {
		return // ilk bağlantı: güncelleme değil
	}
	now := s.now().Unix()
	detail := old + " → " + newVersion
	switch {
	case p.UpdateStatus == store.UpdateStatusUpdating && p.UpdateTarget == newVersion:
		detail += " (otomatik güncelleme)"
	case agentupdate.Newer(newVersion, old):
		detail += " (sürüm düştü)"
	}
	s.log.Info("ajan sürümü değişti", "ajan", p.Name, "eski", old, "yeni", newVersion)
	if err := s.store.SetProbeUpdateState(ctx, p.ID, "", "", "", now); err != nil {
		s.log.Warn("ajan güncelleme durumu temizlenemedi", "hata", err)
	}
	if err := s.store.AddAudit(ctx, store.AuditEntry{Time: now, Action: "agent.update", TargetType: "probe",
		TargetID: p.ID, TargetName: p.Name, Detail: detail, IP: ip}); err != nil {
		s.log.Error("işlem kaydı yazılamadı", "işlem", "agent.update", "hata", err)
	}
	if p.Kind == store.ProbeKindServer {
		s.servers.Publish(ctx, p.ID)
	}
}

// Uç noktalar -----------------------------------------------------------------------------

// probeUpdateReport ajanın durum raporu (POST /api/probe/update).
func (s *Server) probeUpdateReport(w http.ResponseWriter, r *http.Request) {
	p := probeFrom(r)
	var in agentupdate.Report
	if !readJSON(w, r, &in) {
		return
	}
	in.Version = cleanVersion(in.Version)
	note := cleanProbeText(in.Error, agentUpdateNoteMax)
	now := s.now().Unix()
	var status string
	switch in.Status {
	case agentupdate.StatusStarted:
		status = store.UpdateStatusUpdating
		s.log.Info("ajan güncellemeye başladı", "ajan", p.Name, "eski", p.Version, "yeni", in.Version)
	case agentupdate.StatusFailed, agentupdate.StatusRolledBack:
		status = store.UpdateStatusFailed
		if in.Status == agentupdate.StatusRolledBack {
			note = "yeni sürüm çalışmadı, eskisine dönüldü: " + note
		}
		s.log.Warn("ajan güncellemesi başarısız", "ajan", p.Name, "hedef", in.Version, "neden", note)
		if err := s.store.AddAudit(r.Context(), store.AuditEntry{Time: now, Action: "agent.update_failed", TargetType: "probe",
			TargetID: p.ID, TargetName: p.Name, Detail: p.Version + " → " + in.Version + ": " + note, IP: clientIP(r)}); err != nil {
			s.log.Error("işlem kaydı yazılamadı", "işlem", "agent.update_failed", "hata", err)
		}
	default:
		writeError(w, http.StatusBadRequest, "Durum started, failed veya rolled_back olmalı")
		return
	}
	if err := s.store.SetProbeUpdateState(r.Context(), p.ID, status, in.Version, note, now); err != nil {
		s.dbError(w, err)
		return
	}
	if p.Kind == store.ProbeKindServer {
		s.servers.Publish(r.Context(), p.ID)
	}
	w.WriteHeader(http.StatusNoContent)
}

// requestProbeUpdate "Şimdi güncelle": ajan ayarı kapalı olsa da bir kez
// teklif edilir; bekleyen uzun yoklama hemen uyandırılır.
func (s *Server) requestProbeUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	p, err := s.store.GetProbe(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if msg := s.updateRefusal(p); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.RequestProbeUpdate(r.Context(), id, s.now().Unix()); err != nil {
		s.dbError(w, err)
		return
	}
	s.engine.JobsChanged()
	s.audit(r, store.User{}, "probe.update_request", auditTarget(p.Kind), id, p.Name, p.Version+" → "+s.version)
	if p.Kind == store.ProbeKindServer {
		s.servers.Publish(r.Context(), id)
	}
	s.respondProbe(w, r, id)
}

// updateRefusal güncelleme istenemiyorsa nedenini (Türkçe; yanıt dili
// katmanı çevirir) döner.
func (s *Server) updateRefusal(p store.Probe) string {
	st := s.agentUpdateStatus(p)
	switch st.State {
	case agentupdate.StateOutdated, agentupdate.StateFailed, agentupdate.StateUpdating:
		return "" // teklif edilebilir (başarısız/geri dönmeyen ajana yeniden)
	case agentupdate.StateCurrent:
		return "Ajan zaten güncel"
	case agentupdate.StateUnsupported:
		return "Bu ajan otomatik güncellemeyi desteklemiyor; kurulum komutunu yeniden çalıştırın"
	case agentupdate.StateUnsigned:
		return "Panelin bu derlemesi imzasız: ajanlara güncelleme sunulamaz"
	case agentupdate.StateUnversioned:
		return "Sürüm numarası olmayan derlemeler (commit derlemesi) güncellenmez"
	case agentupdate.StateNewer:
		return "Ajan panelden daha yeni bir sürümde"
	case agentupdate.StateOff:
		return "Otomatik güncelleme ajan tarafında kapalı (AUTO_UPDATE=0)"
	case agentupdate.StateReadOnly:
		return "Ajanın program dizini yazılabilir değil; imajı/paketi güncelleyin"
	}
	return "Ajan henüz bağlanmadı"
}

// requestAllProbeUpdates "Tümünü güncelle": verilen türdeki (?kind=server|location)
// güncellenebilir tüm ajanlar için istek bırakır; sayıyı döner.
func (s *Server) requestAllProbeUpdates(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	if !store.ValidProbeKind(kind) {
		writeError(w, http.StatusBadRequest, "kind server veya location olmalı")
		return
	}
	list, err := s.store.ListProbesOfKind(r.Context(), kind)
	if err != nil {
		s.dbError(w, err)
		return
	}
	now := s.now().Unix()
	var names []string
	for _, p := range list {
		if s.updateRefusal(p) != "" {
			continue
		}
		if err := s.store.RequestProbeUpdate(r.Context(), p.ID, now); err != nil {
			s.dbError(w, err)
			return
		}
		names = append(names, p.Name)
		if kind == store.ProbeKindServer {
			s.servers.Publish(r.Context(), p.ID)
		}
	}
	if len(names) > 0 {
		s.engine.JobsChanged()
		s.audit(r, store.User{}, "probe.update_all", auditTarget(kind), 0, "", fmt.Sprintf("%d ajan → %s: %s", len(names), s.version, strings.Join(names, ", ")))
	}
	writeJSON(w, http.StatusOK, map[string]any{"requested": len(names)})
}

// parseAutoUpdate PUT /api/probes/{id} gövdesindeki auto_update alanı:
// true/false ajan başına ayar, null genel ayara dönüş; alan yoksa değişmez.
func parseAutoUpdate(raw json.RawMessage) (val *bool, set bool, err error) {
	if len(raw) == 0 {
		return nil, false, nil
	}
	if string(raw) == "null" {
		return nil, true, nil
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, false, errors.New("auto_update true, false veya null olmalı")
	}
	return &b, true, nil
}
