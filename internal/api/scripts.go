package api

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kadirsungurlu/bekci/internal/betik"
	"github.com/kadirsungurlu/bekci/internal/betikkayit"
	"github.com/kadirsungurlu/bekci/internal/i18n"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// Betik kütüphanesi ve çalıştırma uçları (docs/BETIK-GUVENLIK-680.md §7).
//
// Buradaki denetimler güvenlik sınırı DEĞİLDİR: ele geçirilmiş panel hepsini
// atlayabilir, gerçek karar ajandadır (imza, yerel izin, bilet). Bunlar çalınan
// oturumu, kazayı ve içeriden kötü niyeti yakalar.

const (
	// scriptUserHourly bir kullanıcının saatte en çok başlatma sayısı.
	scriptUserHourly = 20
	// scriptQueueTTL bilet gerektirmeyen işin teslim bekleme süresi.
	scriptQueueTTL = time.Hour
	// scriptNoReceipt ajan kabul ettikten sonra makbuz beklenen en uzun süre:
	// timeout üst sınırı (1 sa) + ajanın disk kuyruğu ile yeniden deneme payı.
	scriptNoReceipt = 6 * time.Hour
	scriptLogKeep   = 365 * 24 * time.Hour // D10
	scriptOutKeep   = 90 * 24 * time.Hour  // D10
	maxScriptBody   = 64 << 10
)

func init() {
	RegisterRoutes(func(s *Server, mux *http.ServeMux) {
		// Okuma: gerçek yönetici oturumu yeter (2FA kurulmamışsa arayüz yönlendirir).
		mux.Handle("GET /api/scripts", s.humanRead(s.listScripts))
		mux.Handle("GET /api/scripts/hosts", s.humanRead(s.listScriptHosts))
		mux.Handle("GET /api/scripts/keys", s.humanRead(s.listScriptKeys))
		mux.Handle("GET /api/scripts/runs", s.humanRead(s.listScriptRunsH))
		mux.Handle("GET /api/scripts/runs/{run}", s.humanRead(s.getScriptRun))
		mux.Handle("GET /api/scripts/chain", s.humanRead(s.scriptChain))
		mux.Handle("GET /api/scripts/export", s.humanRead(s.scriptExport))
		mux.Handle("GET /api/scripts/lib/{id}/{version}", s.humanRead(s.getScriptVersion))
		// Acil durdurma yetki vermez: 2FA'sız oturum da durdurabilir, kod istemez.
		mux.Handle("POST /api/scripts/control/stop", s.humanRead(s.scriptStop))
		mux.Handle("POST /api/scripts/runs/{run}/cancel", s.humanRead(s.cancelScriptRun))
		mux.Handle("POST /api/scripts/runs/{run}/reject", s.humanRead(s.rejectScriptRun))

		mux.Handle("POST /api/scripts", s.human(s.uploadScript))
		mux.Handle("POST /api/scripts/revokes", s.human(s.uploadRevoke))
		mux.Handle("POST /api/scripts/keys", s.human(s.addScriptKey))
		mux.Handle("DELETE /api/scripts/keys/{id}", s.human(s.deleteScriptKey))
		mux.Handle("POST /api/scripts/hosts/{id}/approve", s.human(s.approveScriptHost))
		mux.Handle("POST /api/scripts/runs", s.human(s.startScriptRun))
		mux.Handle("POST /api/scripts/runs/{run}/approve", s.human(s.approveScriptRun))
		mux.Handle("POST /api/scripts/runs/{run}/ticket", s.human(s.ticketScriptRun))
		mux.Handle("POST /api/scripts/control/resume", s.human(s.scriptResume))
		mux.Handle("PUT /api/scripts/control/channels", s.human(s.scriptChannels))
	})
}

func scriptErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": msg, "code": code})
}

// humanRead yönetici + gerçek oturum: API anahtarı hiçbir yoldan geçmez. Yol
// sessionOnly listesindedir; bu ikinci denetim, çerez + Bearer birlikte geldiğinde
// ya da liste bir gün gevşetilirse de kapıyı kapalı tutar (K1).
func (s *Server) humanRead(h http.HandlerFunc) http.Handler {
	return s.admin(func(w http.ResponseWriter, r *http.Request) {
		sess, _ := r.Context().Value(sessionKey).(string)
		if userFrom(r).APIKeyName != "" || sess == "" || r.Header.Get("Authorization") != "" {
			scriptErr(w, http.StatusForbidden, "session_required", "Bu işlem yalnız tarayıcı oturumuyla yapılır")
			return
		}
		if !s.store.Postgres() {
			scriptErr(w, http.StatusConflict, "postgres_required", "Betik özelliği PostgreSQL ister")
			return
		}
		h(w, r)
	}).(http.Handler)
}

// human humanRead + 2FA açık hesap. Başlatma, onay, kütüphane ve güven değişikliği.
func (s *Server) human(h http.HandlerFunc) http.Handler {
	return s.humanRead(func(w http.ResponseWriter, r *http.Request) {
		if !userFrom(r).TwoFactorEnabled {
			scriptErr(w, http.StatusForbidden, "two_factor_required", "Bu işlem için iki adımlı doğrulama açık olmalı")
			return
		}
		h(w, r)
	})
}

// stepUp istek başına TOTP: önbellek yok, kod tek kullanımlık (UseTOTPStep),
// kurtarma kodu kabul edilmez. Yanlış kod giriş sınırlayıcısına işler.
func (s *Server) stepUp(w http.ResponseWriter, r *http.Request, code string) bool {
	u := userFrom(r)
	if s.limited(w, clientIP(r), u.Username) {
		return false
	}
	code = normalizeCode(code)
	if !isTOTPFormat(code) {
		scriptErr(w, http.StatusBadRequest, "step_up_required", "Doğrulama kodu gerekli")
		return false
	}
	_, ok, err := s.verifySecondFactor(r, u.ID, code)
	if err != nil {
		s.dbError(w, err)
		return false
	}
	if !ok {
		s.limiter.fail(clientIP(r), u.Username, s.now())
		scriptErr(w, http.StatusBadRequest, "step_up_failed", "Doğrulama kodu hatalı veya daha önce kullanılmış")
		return false
	}
	return true
}

// ---- Kütüphane ----

type scriptVersionView struct {
	ScriptID       string          `json:"script_id"`
	Version        int64           `json:"version"`
	SHA256         string          `json:"sha256"`
	Approval       string          `json:"approval"`
	KeyID          string          `json:"key_id"`
	NotBefore      int64           `json:"not_before"`
	NotAfter       int64           `json:"not_after"`
	UploadedByName string          `json:"uploaded_by_name"`
	UploadedAt     int64           `json:"uploaded_at"`
	Manifest       json.RawMessage `json:"manifest"`
	Revoked        bool            `json:"revoked"`
	Expired        bool            `json:"expired"`
	Body           *string         `json:"body,omitempty"`
}

type scriptView struct {
	ID       string              `json:"id"`
	Latest   scriptVersionView   `json:"latest"`
	Versions []scriptVersionView `json:"versions"`
	// Hosts bu betiğin sunuculardaki son sonucu. Panel ajanın yerel iznini
	// bilmez; buradaki bilgi geçmişten türer ve kesin hüküm değildir.
	Hosts []scriptHostResult `json:"hosts"`
}

type scriptHostResult struct {
	ProbeID   int64  `json:"probe_id"`
	ProbeName string `json:"probe_name"`
	State     string `json:"state"`
	Code      string `json:"code,omitempty"`
	At        int64  `json:"at"`
}

func revokedBelow(list []store.ScriptRevoke, id string) int64 {
	var v int64
	for _, r := range list {
		if r.ScriptID == id {
			v = max(v, r.BelowVersion)
		}
	}
	return v
}

func (s *Server) scriptVersionOf(r store.ScriptRow, below int64) scriptVersionView {
	return scriptVersionView{ScriptID: r.ScriptID, Version: r.Version, SHA256: r.SHA256, Approval: r.Approval, KeyID: r.KeyID,
		NotBefore: r.NotBefore, NotAfter: r.NotAfter, UploadedByName: r.UploadedByName, UploadedAt: r.UploadedAt,
		Manifest: json.RawMessage(r.Manifest), Revoked: r.Version < below, Expired: s.now().Unix() >= r.NotAfter}
}

func (s *Server) listScripts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := s.store.ListScripts(ctx)
	if err != nil {
		s.dbError(w, err)
		return
	}
	revokes, err := s.store.ListScriptRevokes(ctx)
	if err != nil {
		s.dbError(w, err)
		return
	}
	ctl, err := s.store.ScriptControlState(ctx)
	if err != nil {
		s.dbError(w, err)
		return
	}
	recent, err := s.store.ListScriptRuns(ctx, store.ScriptRunFilter{Limit: 200})
	if err != nil {
		s.dbError(w, err)
		return
	}
	// Betik + sunucu başına en yeni sonuç (liste yeniden eskiye).
	results := map[string][]scriptHostResult{}
	seen := map[string]bool{}
	for _, sum := range recent {
		v := runViewOf(sum.Requested, sum.Last)
		if k := v.ScriptID + "/" + strconv.FormatInt(v.ProbeID, 10); !seen[k] {
			seen[k] = true
			results[v.ScriptID] = append(results[v.ScriptID], scriptHostResult{v.ProbeID, v.ProbeName, v.State, v.Code, v.UpdatedAt})
		}
	}
	list := []scriptView{}
	for _, row := range rows {
		vv := s.scriptVersionOf(row, revokedBelow(revokes, row.ScriptID))
		if n := len(list); n > 0 && list[n-1].ID == row.ScriptID {
			list[n-1].Versions = append(list[n-1].Versions, vv)
			continue
		}
		hosts := results[row.ScriptID]
		if hosts == nil {
			hosts = []scriptHostResult{}
		}
		list = append(list, scriptView{ID: row.ScriptID, Latest: vv, Versions: []scriptVersionView{vv}, Hosts: hosts})
	}
	writeJSON(w, http.StatusOK, map[string]any{"scripts": list, "revokes": revokes, "control": ctl})
}

func (s *Server) getScriptVersion(w http.ResponseWriter, r *http.Request) {
	v, err := strconv.ParseInt(r.PathValue("version"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Geçersiz sürüm")
		return
	}
	row, err := s.store.GetScript(r.Context(), r.PathValue("id"), v)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "Bulunamadı")
		return
	}
	if err != nil {
		s.dbError(w, err)
		return
	}
	revokes, err := s.store.ListScriptRevokes(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	vv := s.scriptVersionOf(row, revokedBelow(revokes, row.ScriptID))
	body := string(row.Body)
	vv.Body = &body
	writeJSON(w, http.StatusOK, vv)
}

func (s *Server) trustKeys(ctx context.Context, slot string) ([]ed25519.PublicKey, error) {
	list, err := s.store.ListScriptKeys(ctx)
	if err != nil {
		return nil, err
	}
	var out []ed25519.PublicKey
	for _, k := range list {
		if k.Slot != slot {
			continue
		}
		if pub, err := betik.ParsePublicKey(k.PublicKey); err == nil {
			out = append(out, pub)
		}
	}
	return out, nil
}

// uploadScript imzalı zarf + gövde yükler; panel betik düzenlemez. İmza burada
// yalnız yanlış dosyayı erken yakalamak için doğrulanır, ajan yine doğrular.
func (s *Server) uploadScript(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Manifest json.RawMessage `json:"manifest"`
		Body     []byte          `json:"body"`
		Code     string          `json:"code"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	env, err := betik.ParseEnvelope(in.Manifest)
	if err != nil {
		scriptErr(w, http.StatusUnprocessableEntity, betik.CodeEnvelopeInvalid, "Zarf okunamadı")
		return
	}
	ctx := r.Context()
	keys, err := s.trustKeys(ctx, "yayin")
	if err != nil {
		s.dbError(w, err)
		return
	}
	if !env.VerifyAny(betik.KindManifest, keys) {
		scriptErr(w, http.StatusUnprocessableEntity, betik.CodeSigInvalid, "İmza panelin bildiği yayın anahtarlarıyla doğrulanamadı")
		return
	}
	m, err := betik.ParseManifest(env.Payload)
	if err != nil {
		scriptErr(w, http.StatusUnprocessableEntity, betik.CodePayloadInvalid, "Bildirim geçersiz")
		return
	}
	if len(in.Body) == 0 || len(in.Body) > maxScriptBody {
		scriptErr(w, http.StatusUnprocessableEntity, betik.CodeBodyTooLarge, "Gövde boş ya da çok büyük")
		return
	}
	if betik.Digest(in.Body) != m.SHA256 {
		scriptErr(w, http.StatusUnprocessableEntity, betik.CodeBodyMismatch, "Gövde özeti bildirimle tutmuyor")
		return
	}
	if !s.stepUp(w, r, in.Code) {
		return
	}
	u := userFrom(r)
	row := store.ScriptRow{ScriptID: m.ID, Version: m.Version, Envelope: string(in.Manifest), Body: in.Body, SHA256: m.SHA256,
		Approval: m.Approval, Manifest: string(env.Payload), KeyID: env.Key, NotBefore: m.NotBefore, NotAfter: m.NotAfter,
		UploadedBy: u.ID, UploadedByName: u.Username, UploadedAt: s.now().Unix()}
	if err := s.store.AddScript(ctx, row); err != nil {
		if errors.Is(err, store.ErrScriptExists) {
			scriptErr(w, http.StatusConflict, "version_exists", "Bu sürüm zaten yüklü")
			return
		}
		s.dbError(w, err)
		return
	}
	s.audit(r, u, "script.upload", "script", 0, m.ID, "v"+strconv.FormatInt(m.Version, 10))
	writeJSON(w, http.StatusCreated, s.scriptVersionOf(row, 0))
}

func (s *Server) uploadRevoke(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Revoke json.RawMessage `json:"revoke"`
		Code   string          `json:"code"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	env, err := betik.ParseEnvelope(in.Revoke)
	if err != nil {
		scriptErr(w, http.StatusUnprocessableEntity, betik.CodeEnvelopeInvalid, "Zarf okunamadı")
		return
	}
	keys, err := s.trustKeys(r.Context(), "yayin")
	if err != nil {
		s.dbError(w, err)
		return
	}
	if !env.VerifyAny(betik.KindRevoke, keys) {
		scriptErr(w, http.StatusUnprocessableEntity, betik.CodeSigInvalid, "İmza doğrulanamadı")
		return
	}
	rv, err := betik.ParseRevoke(env.Payload)
	if err != nil {
		scriptErr(w, http.StatusUnprocessableEntity, betik.CodePayloadInvalid, "İptal geçersiz")
		return
	}
	if !s.stepUp(w, r, in.Code) {
		return
	}
	u := userFrom(r)
	if err := s.store.AddScriptRevoke(r.Context(), store.ScriptRevoke{ScriptID: rv.ID, BelowVersion: rv.BelowVersion,
		Envelope: string(in.Revoke), UploadedByName: u.Username, UploadedAt: s.now().Unix()}); err != nil {
		s.dbError(w, err)
		return
	}
	s.audit(r, u, "script.revoke", "script", 0, rv.ID, "<v"+strconv.FormatInt(rv.BelowVersion, 10))
	s.engine.JobsChanged()
	writeJSON(w, http.StatusCreated, map[string]bool{"ok": true})
}

// ---- Güven anahtarları ----

func (s *Server) listScriptKeys(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListScriptKeys(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) addScriptKey(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Slot      string `json:"slot"`
		PublicKey string `json:"public_key"`
		Name      string `json:"name"`
		Code      string `json:"code"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Slot != "yayin" && in.Slot != "onay" || in.Name == "" || len(in.Name) > 64 {
		scriptErr(w, http.StatusBadRequest, "invalid", "Geçersiz anahtar bilgisi")
		return
	}
	pub, err := betik.ParsePublicKey(strings.TrimSpace(in.PublicKey))
	if err != nil {
		scriptErr(w, http.StatusBadRequest, "key_invalid", "Açık anahtar okunamadı")
		return
	}
	ctx := r.Context()
	id := betik.KeyID(pub)
	list, err := s.store.ListScriptKeys(ctx)
	if err != nil {
		s.dbError(w, err)
		return
	}
	for _, k := range list {
		if k.KeyID == id {
			scriptErr(w, http.StatusConflict, "key_exists", "Bu anahtar zaten kayıtlı; iki yuvada aynı anahtar kullanılamaz")
			return
		}
	}
	if !s.stepUp(w, r, in.Code) {
		return
	}
	u := userFrom(r)
	k := store.ScriptKey{KeyID: id, Slot: in.Slot, PublicKey: betik.EncodePublicKey(pub), Name: in.Name, AddedByName: u.Username, AddedAt: s.now().Unix()}
	if err := s.store.AddScriptKey(ctx, k); err != nil {
		s.dbError(w, err)
		return
	}
	s.audit(r, u, "script.key_add", "script_key", 0, in.Name, in.Slot+" "+id[:16])
	writeJSON(w, http.StatusCreated, k)
}

func (s *Server) deleteScriptKey(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code string `json:"code"`
	}
	if !readJSON(w, r, &in) || !s.stepUp(w, r, in.Code) {
		return
	}
	id := r.PathValue("id")
	ok, err := s.store.DeleteScriptKey(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "Bulunamadı")
		return
	}
	s.audit(r, userFrom(r), "script.key_delete", "script_key", 0, id, "")
	w.WriteHeader(http.StatusNoContent)
}

// ---- Sunucu kimlikleri ----

type scriptHostView struct {
	store.ScriptHost
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Online bool   `json:"online"`
	// Support: unsupported (başlık hiç gelmedi, eski ajan) | off | on
	Support string `json:"support"`
}

func (s *Server) listScriptHosts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	probes, err := s.store.ListProbes(ctx)
	if err != nil {
		s.dbError(w, err)
		return
	}
	hosts, err := s.store.ListScriptHosts(ctx)
	if err != nil {
		s.dbError(w, err)
		return
	}
	byID := map[int64]store.ScriptHost{}
	for _, h := range hosts {
		byID[h.ProbeID] = h
	}
	out := []scriptHostView{}
	for _, p := range probes {
		h, ok := byID[p.ID]
		if !ok {
			h = store.ScriptHost{ProbeID: p.ID}
		}
		v := scriptHostView{ScriptHost: h, Name: p.Name, Kind: p.Kind, Online: s.engine.ProbeOnline(p, s.now()), Support: "unsupported"}
		if h.State != "" {
			v.Support = h.State
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) approveScriptHost(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Geçersiz kimlik")
		return
	}
	var in struct {
		HostKey string `json:"host_key"`
		Code    string `json:"code"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	ctx := r.Context()
	h, err := s.store.ScriptHostInfo(ctx, id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if h.PendingKey == "" || h.PendingKey != in.HostKey {
		scriptErr(w, http.StatusConflict, "key_changed", "Bekleyen anahtar değişmiş; sayfayı yenileyin")
		return
	}
	if !s.stepUp(w, r, in.Code) {
		return
	}
	u := userFrom(r)
	var ok bool
	err = s.store.ScriptTx(ctx, func(l *store.ScriptLog) error {
		pub, done, e := l.ApproveHostKey(ctx, id, in.HostKey, u.Username, s.now().Unix())
		if e != nil || !done {
			return e
		}
		ok = true
		_, e = l.Append(ctx, "", "host_key_approved", u.ID, map[string]any{"probe_id": id, "old": h.HostKey, "host_key": in.HostKey, "public_key": pub, "by": u.Username})
		return e
	})
	if err != nil {
		s.dbError(w, err)
		return
	}
	if !ok {
		scriptErr(w, http.StatusConflict, "key_changed", "Bekleyen anahtar değişmiş; sayfayı yenileyin")
		return
	}
	s.audit(r, u, "script.host_approve", "probe", id, in.HostKey[:16], "")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- Çalıştırma görünümü ----

// runRequest "requested" olayının verisi; çalıştırmanın tüm değişmez alanları.
type runRequest struct {
	ScriptID       string `json:"script_id"`
	Version        int64  `json:"version"`
	ProbeID        int64  `json:"probe_id"`
	ProbeName      string `json:"probe_name"`
	HostKey        string `json:"host_key"`
	Params         string `json:"params"`
	ParamsSHA256   string `json:"params_sha256"`
	ManifestSHA256 string `json:"manifest_sha256"`
	Approval       string `json:"approval"`
	Needs          string `json:"needs"` // none | panel | signed
	By             string `json:"by"`
}

type runView struct {
	RunID       string `json:"run_id"`
	ScriptID    string `json:"script_id"`
	Version     int64  `json:"version"`
	ProbeID     int64  `json:"probe_id"`
	ProbeName   string `json:"probe_name"`
	Params      string `json:"params"`
	Approval    string `json:"approval"`
	Needs       string `json:"needs"`
	State       string `json:"state"`
	Code        string `json:"code,omitempty"`
	ExitCode    *int   `json:"exit_code,omitempty"`
	RequestedBy string `json:"requested_by"`
	RequestedAt int64  `json:"requested_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

// Çalıştırma durumları; son olaydan türer.
const (
	runAwaitApproval = "awaiting_approval"
	runAwaitTicket   = "awaiting_ticket"
	runQueued        = "queued"
	runAccepted      = "accepted"
	runCancelling    = "cancelling"
	runOK            = "ok"
	runFailed        = "failed"
	runTimeout       = "timeout"
	runCancelled     = "cancelled"
	runLost          = "lost"
	runRejected      = "rejected"       // insan reddetti
	runAgentRejected = "agent_rejected" // ajan reddetti
	runExpired       = "expired"
)

func runStateOf(req runRequest, last betikkayit.Event) (state, code string, exit *int) {
	var d struct {
		Status   string `json:"status"`
		ExitCode *int   `json:"exit_code"`
		Code     string `json:"code"`
	}
	_ = json.Unmarshal([]byte(last.Data), &d)
	switch last.Kind {
	case "requested":
		switch req.Needs {
		case "panel":
			return runAwaitApproval, "", nil
		case "signed":
			return runAwaitTicket, "", nil
		}
		return runQueued, "", nil
	case "approved", "ticket", "queued":
		return runQueued, "", nil
	case "accepted":
		return runAccepted, "", nil
	case "cancel_requested":
		return runCancelling, "", nil
	case "receipt":
		return d.Status, "", d.ExitCode
	case "rejected":
		return runRejected, "", nil
	case "agent_rejected":
		return runAgentRejected, d.Code, nil
	case "canceled":
		return runCancelled, "", nil
	case "expired":
		return runExpired, "", nil
	case "no_receipt":
		return runLost, "", nil
	}
	return last.Kind, "", nil
}

func runViewOf(reqEv, last betikkayit.Event) runView {
	var req runRequest
	_ = json.Unmarshal([]byte(reqEv.Data), &req)
	state, code, exit := runStateOf(req, last)
	return runView{RunID: reqEv.RunID, ScriptID: req.ScriptID, Version: req.Version, ProbeID: req.ProbeID, ProbeName: req.ProbeName,
		Params: req.Params, Approval: req.Approval, Needs: req.Needs, State: state, Code: code, ExitCode: exit,
		RequestedBy: req.By, RequestedAt: reqEv.Time, UpdatedAt: last.Time}
}

func (s *Server) listScriptRunsH(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
	sums, err := s.store.ListScriptRuns(r.Context(), store.ScriptRunFilter{Before: before, Limit: limit})
	if err != nil {
		s.dbError(w, err)
		return
	}
	out := []runView{}
	var next int64
	for _, sum := range sums {
		out = append(out, runViewOf(sum.Requested, sum.Last))
		next = sum.Requested.Seq
	}
	if len(sums) < limit {
		next = 0
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": out, "next": next})
}

func (s *Server) getScriptRun(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	runID := r.PathValue("run")
	if !betik.IsRunID(runID) {
		writeError(w, http.StatusBadRequest, "Geçersiz çalıştırma")
		return
	}
	events, err := s.store.ScriptRunEvents(ctx, runID)
	if err != nil {
		s.dbError(w, err)
		return
	}
	var reqEv betikkayit.Event
	for _, e := range events {
		if e.Kind == "requested" {
			reqEv = e
			break
		}
	}
	if reqEv.RunID == "" {
		writeError(w, http.StatusNotFound, "Bulunamadı")
		return
	}
	v := runViewOf(reqEv, events[len(events)-1])
	out := map[string]any{"run": v, "events": events}
	var req runRequest
	_ = json.Unmarshal([]byte(reqEv.Data), &req)
	if v.State == runAwaitTicket {
		// bekci-imza bilet girdisi: operatör bunu kendi makinesinde okuyup imzalar.
		out["ticket_request"] = map[string]any{"run_id": runID, "host_key": req.HostKey, "script_id": req.ScriptID,
			"version": req.Version, "manifest_sha256": req.ManifestSHA256, "params_sha256": req.ParamsSHA256, "params": req.Params}
	}
	u := userFrom(r)
	out["can_approve"] = v.State == runAwaitApproval && reqEv.ActorID != u.ID && u.TwoFactorEnabled
	if b, sha, tr, ok, err := s.store.ScriptOutput(ctx, runID); err != nil {
		s.dbError(w, err)
		return
	} else if ok {
		out["output"] = map[string]any{"text": string(b), "sha256": sha, "truncated": tr}
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- Başlatma ve onay ----

// otherApprover başlatan dışında panel onayı verebilecek bir yönetici var mı (D3).
func (s *Server) otherApprover(ctx context.Context, self int64) (bool, error) {
	users, err := s.store.ListUsers(ctx)
	if err != nil {
		return false, err
	}
	for _, u := range users {
		if u.ID != self && u.Role == store.RoleAdmin && !u.Disabled && u.TwoFactorEnabled {
			return true, nil
		}
	}
	return false, nil
}

func newRunID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

func schemaSupported(schemas string, v int) bool {
	for _, x := range strings.Split(schemas, ",") {
		if strings.TrimSpace(x) == strconv.Itoa(v) {
			return true
		}
	}
	return false
}

func (s *Server) startScriptRun(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ScriptID  string          `json:"script_id"`
		Version   int64           `json:"version"`
		ProbeID   int64           `json:"probe_id"` // tek istek = tek sunucu: toplu çalıştırma yok
		Params    json.RawMessage `json:"params"`
		Code      string          `json:"code"`
		UseTicket bool            `json:"use_ticket"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	ctx := r.Context()
	u := userFrom(r)
	now := s.now()
	ctl, err := s.store.ScriptControlState(ctx)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if ctl.Stopped {
		scriptErr(w, http.StatusConflict, "stopped", "Betikler acil durdurmada")
		return
	}
	row, err := s.store.GetScript(ctx, in.ScriptID, in.Version)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "Betik bulunamadı")
		return
	}
	if err != nil {
		s.dbError(w, err)
		return
	}
	m, err := betik.ParseManifest([]byte(row.Manifest))
	if err != nil {
		scriptErr(w, http.StatusUnprocessableEntity, betik.CodePayloadInvalid, "Bildirim geçersiz")
		return
	}
	revokes, err := s.store.ListScriptRevokes(ctx)
	if err != nil {
		s.dbError(w, err)
		return
	}
	switch {
	case row.Version < revokedBelow(revokes, row.ScriptID):
		scriptErr(w, http.StatusConflict, betik.CodeRevoked, "Bu sürüm iptal edilmiş")
		return
	case now.Unix() >= m.NotAfter:
		scriptErr(w, http.StatusConflict, betik.CodeExpired, "İmzanın süresi dolmuş")
		return
	case now.Unix() < m.NotBefore:
		scriptErr(w, http.StatusConflict, betik.CodeNotYet, "İmza henüz geçerli değil")
		return
	}
	p, err := s.store.GetProbe(ctx, in.ProbeID)
	if err != nil {
		s.dbError(w, err)
		return
	}
	h, err := s.store.ScriptHostInfo(ctx, p.ID)
	if err != nil {
		s.dbError(w, err)
		return
	}
	switch {
	case h.State == "":
		scriptErr(w, http.StatusConflict, "host_unsupported", "Bu ajan betik desteklemiyor")
		return
	case h.State != "on":
		scriptErr(w, http.StatusConflict, "host_off", "Bu sunucuda betik kapalı")
		return
	case h.HostKey == "":
		scriptErr(w, http.StatusConflict, "host_unapproved", "Sunucunun anahtarı onaylanmamış")
		return
	case h.PendingKey != "":
		scriptErr(w, http.StatusConflict, "host_key_pending", "Sunucunun anahtarı değişmiş, onay bekliyor")
		return
	case !schemaSupported(h.Schemas, m.Schema):
		scriptErr(w, http.StatusConflict, "schema_unsupported", "Ajan bu bildirim sürümünü desteklemiyor")
		return
	}
	raw := bytes.TrimSpace(in.Params)
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if _, err := betik.ParseParams(m, raw); err != nil {
		scriptErr(w, http.StatusUnprocessableEntity, betik.CodeParamsInvalid, "Parametreler şemaya uymuyor")
		return
	}
	n, err := s.store.ScriptRequestsSince(ctx, u.ID, now.Add(-time.Hour).Unix())
	if err != nil {
		s.dbError(w, err)
		return
	}
	if n >= scriptUserHourly {
		s.scriptNotify(ctx, p, "", "script.rate_limit", u.Username)
		scriptErr(w, http.StatusTooManyRequests, "rate_limited", "Saatlik başlatma sınırı aşıldı")
		return
	}
	needs := m.Approval
	if needs == betik.ApprovalPanel {
		ok, err := s.otherApprover(ctx, u.ID)
		if err != nil {
			s.dbError(w, err)
			return
		}
		if !ok {
			needs = betik.ApprovalSigned // D3
		}
	}
	if in.UseTicket {
		needs = betik.ApprovalSigned
	}
	if !s.stepUp(w, r, in.Code) {
		return
	}
	env, err := betik.ParseEnvelope([]byte(row.Envelope))
	if err != nil {
		scriptErr(w, http.StatusUnprocessableEntity, betik.CodeEnvelopeInvalid, "Zarf okunamadı")
		return
	}
	runID := newRunID()
	req := runRequest{ScriptID: m.ID, Version: m.Version, ProbeID: p.ID, ProbeName: p.Name, HostKey: h.HostKey, Params: string(raw),
		ParamsSHA256: betik.Digest(raw), ManifestSHA256: env.PayloadDigest(), Approval: m.Approval, Needs: needs, By: u.Username}
	var reqEv, last betikkayit.Event
	err = s.store.ScriptTx(ctx, func(l *store.ScriptLog) error {
		var e error
		if reqEv, e = l.Append(ctx, runID, "requested", u.ID, req); e != nil {
			return e
		}
		last = reqEv
		if needs != betik.ApprovalNone {
			return nil
		}
		last, e = s.enqueue(ctx, l, runID, req, "", now.Add(scriptQueueTTL).Unix())
		return e
	})
	var se *scriptStateErr
	if errors.As(err, &se) {
		scriptErr(w, http.StatusConflict, se.code, se.msg)
		return
	}
	if err != nil {
		s.dbError(w, err)
		return
	}
	s.audit(r, u, "script.start", "probe", p.ID, m.ID, fmt.Sprintf("v%d %s run=%s", m.Version, needs, runID))
	s.scriptNotify(ctx, p, runID, "script.started", m.ID, u.Username)
	s.engine.JobsChanged()
	writeJSON(w, http.StatusCreated, runViewOf(reqEv, last))
}

func (s *Server) enqueue(ctx context.Context, l *store.ScriptLog, runID string, req runRequest, ticket string, expires int64) (betikkayit.Event, error) {
	// Zincir kilidi altında: istek başındaki denetimle arada gelen acil durdurma
	// kaçmasın, iş durdurma kalkınca kendiliğinden çalışmasın.
	stopped, err := l.Stopped(ctx)
	if err != nil {
		return betikkayit.Event{}, err
	}
	if stopped {
		return betikkayit.Event{}, &scriptStateErr{"stopped", "Betikler acil durdurmada"}
	}
	if err := l.Enqueue(ctx, store.ScriptJob{RunID: runID, ProbeID: req.ProbeID, ScriptID: req.ScriptID, Version: req.Version,
		Params: req.Params, Ticket: ticket, QueuedAt: s.now().Unix(), ExpiresAt: expires}); err != nil {
		return betikkayit.Event{}, err
	}
	return l.Append(ctx, runID, "queued", 0, map[string]any{"expires_at": expires})
}

// runTx çalıştırmanın durumunu zincir kilidi altında okur: durum kontrolü ile
// olay eklemesi arasında yarış yoktur (iki onay, onay + iptal).
func (s *Server) runTx(w http.ResponseWriter, r *http.Request, fn func(l *store.ScriptLog, reqEv betikkayit.Event, req runRequest, state string) error) (reqEvOut betikkayit.Event, reqOut runRequest, ok bool) {
	ctx := r.Context()
	runID := r.PathValue("run")
	if !betik.IsRunID(runID) {
		writeError(w, http.StatusBadRequest, "Geçersiz çalıştırma")
		return
	}
	err := s.store.ScriptTx(ctx, func(l *store.ScriptLog) error {
		reqEv, e := l.Requested(ctx, runID)
		if e != nil {
			return e
		}
		last, e := l.LastKind(ctx, runID)
		if e != nil {
			return e
		}
		var req runRequest
		if e := json.Unmarshal([]byte(reqEv.Data), &req); e != nil {
			return e
		}
		state, _, _ := runStateOf(req, last)
		reqEvOut, reqOut = reqEv, req
		return fn(l, reqEv, req, state)
	})
	var se *scriptStateErr
	switch {
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "Bulunamadı")
	case errors.As(err, &se):
		scriptErr(w, http.StatusConflict, se.code, se.msg)
	case err != nil:
		s.dbError(w, err)
	default:
		ok = true
	}
	return
}

type scriptStateErr struct{ code, msg string }

func (e *scriptStateErr) Error() string { return e.code }

var errRunState = &scriptStateErr{"run_state", "Çalıştırma bu işlem için uygun durumda değil"}

func (s *Server) scriptStopped(w http.ResponseWriter, r *http.Request) bool {
	ctl, err := s.store.ScriptControlState(r.Context())
	if err != nil {
		s.dbError(w, err)
		return true
	}
	if ctl.Stopped {
		scriptErr(w, http.StatusConflict, "stopped", "Betikler acil durdurmada")
	}
	return ctl.Stopped
}

// approveScriptRun ikinci yönetici onayı: başlatan kendi isteğini onaylayamaz (D3).
func (s *Server) approveScriptRun(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code string `json:"code"`
	}
	if !readJSON(w, r, &in) || s.scriptStopped(w, r) {
		return
	}
	ctx := r.Context()
	u := userFrom(r)
	// Önce kod harcanmadan durum ve kişi denetlenir.
	runID := r.PathValue("run")
	if !betik.IsRunID(runID) {
		writeError(w, http.StatusBadRequest, "Geçersiz çalıştırma")
		return
	}
	events, err := s.store.ScriptRunEvents(ctx, runID)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if len(events) == 0 {
		writeError(w, http.StatusNotFound, "Bulunamadı")
		return
	}
	if events[0].ActorID == u.ID {
		scriptErr(w, http.StatusForbidden, "self_approval", "Kendi isteğinizi onaylayamazsınız")
		return
	}
	if !s.stepUp(w, r, in.Code) {
		return
	}
	reqEv, req, ok := s.runTx(w, r, func(l *store.ScriptLog, reqEv betikkayit.Event, req runRequest, state string) error {
		if state != runAwaitApproval {
			return errRunState
		}
		if reqEv.ActorID == u.ID {
			return &scriptStateErr{"self_approval", "Kendi isteğinizi onaylayamazsınız"}
		}
		if _, e := l.Append(ctx, reqEv.RunID, "approved", u.ID, map[string]any{"by": u.Username}); e != nil {
			return e
		}
		_, e := s.enqueue(ctx, l, reqEv.RunID, req, "", s.now().Add(scriptQueueTTL).Unix())
		return e
	})
	if !ok {
		return
	}
	s.audit(r, u, "script.approve", "probe", req.ProbeID, req.ScriptID, "run="+reqEv.RunID)
	s.notifyRun(ctx, req, reqEv.RunID, "script.approved", req.ScriptID, u.Username)
	s.engine.JobsChanged()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ticketScriptRun bekci-imza bilet çıktısını alır. Panel bileti yalnız yanlış
// yapıştırmayı yakalamak için doğrular; asıl denetim ajandadır.
func (s *Server) ticketScriptRun(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Ticket json.RawMessage `json:"ticket"`
		Code   string          `json:"code"`
	}
	if !readJSON(w, r, &in) || s.scriptStopped(w, r) {
		return
	}
	ctx := r.Context()
	u := userFrom(r)
	runID := r.PathValue("run")
	env, err := betik.ParseEnvelope(in.Ticket)
	if err != nil {
		scriptErr(w, http.StatusUnprocessableEntity, betik.CodeEnvelopeInvalid, "Bilet okunamadı")
		return
	}
	keys, err := s.trustKeys(ctx, "onay")
	if err != nil {
		s.dbError(w, err)
		return
	}
	if !env.VerifyAny(betik.KindTicket, keys) {
		scriptErr(w, http.StatusUnprocessableEntity, betik.CodeTicketSig, "Bilet imzası panelin bildiği onay anahtarlarıyla doğrulanamadı")
		return
	}
	t, err := betik.ParseTicket(env.Payload)
	if err != nil {
		scriptErr(w, http.StatusUnprocessableEntity, betik.CodeTicketInvalid, "Bilet geçersiz")
		return
	}
	if !s.stepUp(w, r, in.Code) {
		return
	}
	reqEv, req, ok := s.runTx(w, r, func(l *store.ScriptLog, reqEv betikkayit.Event, req runRequest, state string) error {
		switch {
		case state != runAwaitTicket:
			return errRunState
		case t.RunID != runID || t.HostKey != req.HostKey:
			return &scriptStateErr{betik.CodeTicketHost, "Bilet bu çalıştırma ya da sunucu için değil"}
		case t.ScriptID != req.ScriptID || t.Version != req.Version || t.ManifestSHA256 != req.ManifestSHA256 || t.ParamsSHA256 != req.ParamsSHA256:
			return &scriptStateErr{betik.CodeTicketMismatch, "Bilet betik ya da parametrelerle tutmuyor"}
		case s.now().Unix() >= t.ExpiresAt:
			return &scriptStateErr{betik.CodeTicketExpired, "Biletin süresi dolmuş"}
		}
		if _, e := l.Append(ctx, runID, "ticket", u.ID, map[string]any{"by": u.Username, "approver": t.Approver, "ticket_sha256": env.PayloadDigest(), "key": env.Key}); e != nil {
			return e
		}
		_, e := s.enqueue(ctx, l, runID, req, string(in.Ticket), t.ExpiresAt)
		return e
	})
	if !ok {
		return
	}
	s.audit(r, u, "script.ticket", "probe", req.ProbeID, req.ScriptID, "run="+reqEv.RunID)
	s.notifyRun(ctx, req, runID, "script.approved", req.ScriptID, t.Approver)
	s.engine.JobsChanged()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) rejectScriptRun(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(r)
	reqEv, req, ok := s.runTx(w, r, func(l *store.ScriptLog, reqEv betikkayit.Event, _ runRequest, state string) error {
		if state != runAwaitApproval && state != runAwaitTicket {
			return errRunState
		}
		_, e := l.Append(ctx, reqEv.RunID, "rejected", u.ID, map[string]any{"by": u.Username})
		return e
	})
	if !ok {
		return
	}
	s.audit(r, u, "script.reject", "probe", req.ProbeID, req.ScriptID, "run="+reqEv.RunID)
	s.notifyRun(ctx, req, reqEv.RunID, "script.rejected", req.ScriptID, u.Username)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) cancelScriptRun(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(r)
	reqEv, req, ok := s.runTx(w, r, func(l *store.ScriptLog, reqEv betikkayit.Event, _ runRequest, state string) error {
		switch state {
		case runAwaitApproval, runAwaitTicket:
			_, e := l.Append(ctx, reqEv.RunID, "canceled", u.ID, map[string]any{"by": u.Username})
			return e
		case runQueued:
			if e := l.Dequeue(ctx, reqEv.RunID); e != nil {
				return e
			}
			_, e := l.Append(ctx, reqEv.RunID, "canceled", u.ID, map[string]any{"by": u.Username})
			return e
		case runAccepted:
			if e := l.RequestCancel(ctx, reqEv.RunID); e != nil {
				return e
			}
			_, e := l.Append(ctx, reqEv.RunID, "cancel_requested", u.ID, map[string]any{"by": u.Username})
			return e
		}
		return errRunState
	})
	if !ok {
		return
	}
	s.audit(r, u, "script.cancel", "probe", req.ProbeID, req.ScriptID, "run="+reqEv.RunID)
	s.engine.JobsChanged()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- Acil durdurma, bildirim, zincir ----

func (s *Server) scriptStop(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(r)
	err := s.store.ScriptTx(ctx, func(l *store.ScriptLog) error {
		pending, running, e := l.SetStopped(ctx, true, u.Username, s.now().Unix())
		if e != nil {
			return e
		}
		for _, id := range pending {
			if _, e := l.Append(ctx, id, "canceled", u.ID, map[string]any{"by": u.Username, "reason": "emergency_stop"}); e != nil {
				return e
			}
		}
		for _, id := range running {
			if _, e := l.Append(ctx, id, "cancel_requested", u.ID, map[string]any{"by": u.Username, "reason": "emergency_stop"}); e != nil {
				return e
			}
		}
		_, e = l.Append(ctx, "", "emergency_stop", u.ID, map[string]any{"by": u.Username})
		return e
	})
	if err != nil {
		s.dbError(w, err)
		return
	}
	s.audit(r, u, "script.stop", "script", 0, "", "")
	s.scriptNotify(ctx, store.Probe{}, "", "script.stopped", u.Username)
	s.engine.JobsChanged()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) scriptResume(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code string `json:"code"`
	}
	if !readJSON(w, r, &in) || !s.stepUp(w, r, in.Code) {
		return
	}
	ctx := r.Context()
	u := userFrom(r)
	err := s.store.ScriptTx(ctx, func(l *store.ScriptLog) error {
		if _, _, e := l.SetStopped(ctx, false, u.Username, s.now().Unix()); e != nil {
			return e
		}
		_, e := l.Append(ctx, "", "emergency_resume", u.ID, map[string]any{"by": u.Username})
		return e
	})
	if err != nil {
		s.dbError(w, err)
		return
	}
	s.audit(r, u, "script.resume", "script", 0, "", "")
	s.scriptNotify(ctx, store.Probe{}, "", "script.resumed", u.Username)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) scriptChannels(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ChannelIDs []int64 `json:"channel_ids"`
		Code       string  `json:"code"`
	}
	if !readJSON(w, r, &in) || !s.stepUp(w, r, in.Code) {
		return
	}
	if in.ChannelIDs == nil {
		in.ChannelIDs = []int64{}
	}
	if err := s.store.SetScriptChannels(r.Context(), in.ChannelIDs); err != nil {
		s.dbError(w, err)
		return
	}
	s.audit(r, userFrom(r), "script.channels", "script", 0, "", "")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// scriptChain zincir başı ve panelin kendi doğrulaması. Arayüzde "panelin
// beyanı" olarak gösterilir: tek otorite çevrimdışı `bekci-imza dogrula`dır (§8.5).
func (s *Server) scriptChain(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	x, err := s.store.ExportScriptLog(ctx)
	if err != nil {
		s.dbError(w, err)
		return
	}
	out := map[string]any{"events": len(x.Events), "checkpoints": len(x.Checkpoints), "ok": true, "seq": 0, "hash": ""}
	if n := len(x.Events); n > 0 {
		out["seq"], out["hash"] = x.Events[n-1].Seq, x.Events[n-1].Hash
	}
	if err := betikkayit.Verify(x); err != nil {
		out["ok"], out["error"] = false, err.Error()
	}
	if a, ok, err := s.store.LastScriptEvent(ctx, "anchor"); err == nil && ok {
		out["last_anchor_at"] = a.Time
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) scriptExport(w http.ResponseWriter, r *http.Request) {
	x, err := s.store.ExportScriptLog(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	s.audit(r, userFrom(r), "script.export", "script", 0, "", "")
	w.Header().Set("Content-Disposition", `attachment; filename="bekci-betik-kayit.json"`)
	writeJSON(w, http.StatusOK, x)
}

// ---- Bildirim ----

// scriptNotify çıktı gövdesi hiçbir zaman bildirime girmez (A3).
func (s *Server) scriptNotify(ctx context.Context, p store.Probe, key, msg string, args ...any) {
	if s.notifier == nil {
		return
	}
	lang := s.notifier.Lang(ctx)
	sargs := make([]any, len(args))
	copy(sargs, args)
	name := p.Name
	if name == "" {
		name = "Bekci"
	}
	s.notifier.Notify(notify.Event{Kind: notify.KindScript, ProbeID: p.ID, MonitorName: name, Mount: key,
		Message: i18n.T(lang, "notify."+msg, sargs...), Time: s.now(), Lang: lang})
}

func (s *Server) notifyRun(ctx context.Context, req runRequest, runID, msg string, args ...any) {
	s.scriptNotify(ctx, store.Probe{ID: req.ProbeID, Name: req.ProbeName}, runID, msg, args...)
}

// ---- Arka plan: süre dolumu, çapa, saklama ----

// scriptHeadFile zincir başının panel dışı (DB dışı) kopyası: veritabanı eski
// yedekten dönerse zincirin geri gittiği buradan anlaşılır (§8.7).
func scriptHeadFile(dataDir string) string { return filepath.Join(dataDir, "betik-zincir-basi.json") }

type scriptHead struct {
	Seq  int64  `json:"seq"`
	Hash string `json:"hash"`
}

// RunScripts betik arka plan işleri. PostgreSQL yoksa hiç çalışmaz.
func (s *Server) RunScripts(ctx context.Context, dataDir string) {
	if !s.store.Postgres() {
		return
	}
	s.scriptForkCheck(ctx, dataDir)
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		s.scriptTick(ctx, dataDir)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Server) scriptForkCheck(ctx context.Context, dataDir string) {
	b, err := os.ReadFile(scriptHeadFile(dataDir))
	if err != nil {
		return
	}
	var saved scriptHead
	if json.Unmarshal(b, &saved) != nil || saved.Seq == 0 {
		return
	}
	hash, found, err := s.store.ScriptEventAt(ctx, saved.Seq)
	if err != nil {
		s.log.Error("betik zinciri denetlenemedi", "hata", err)
		return
	}
	seq, _, err := s.store.ScriptChainHead(ctx)
	if err != nil || (seq >= saved.Seq && (!found || hash == saved.Hash)) {
		return
	}
	err = s.store.ScriptTx(ctx, func(l *store.ScriptLog) error {
		_, e := l.Append(ctx, "", "chain_fork", 0, map[string]any{"seen_seq": saved.Seq, "seen_hash": saved.Hash})
		return e
	})
	if err != nil {
		s.log.Error("betik zincir çatallanması yazılamadı", "hata", err)
		return
	}
	s.log.Warn("betik kaydı geri gitmiş: veritabanı eski bir yedekten dönmüş olabilir", "görülen", saved.Seq, "şimdi", seq)
	s.scriptNotify(ctx, store.Probe{}, "fork", "script.chain_fork", saved.Seq, seq)
	s.scriptAnchor(ctx)
}

// scriptAnchor günlük zincir başını yazar ve bildirim kanalına gönderir (§8.4, D7).
func (s *Server) scriptAnchor(ctx context.Context) {
	var seq int64
	var hash string
	err := s.store.ScriptTx(ctx, func(l *store.ScriptLog) error {
		e, err := l.Append(ctx, "", "anchor", 0, map[string]any{})
		seq, hash = e.Seq, e.Hash
		return err
	})
	if err != nil {
		s.log.Error("betik çapası yazılamadı", "hata", err)
		return
	}
	s.scriptNotify(ctx, store.Probe{}, fmt.Sprintf("anchor-%d", seq), "script.anchor", seq, hash)
}

func (s *Server) scriptTick(ctx context.Context, dataDir string) {
	now := s.now()
	expired, err := s.store.ExpiredScriptJobs(ctx, now.Unix())
	if err == nil {
		for _, id := range expired {
			s.closeStaleRun(ctx, id, "expired", "script.expired")
		}
	}
	stale, err := s.store.StaleScriptJobs(ctx, now.Add(-scriptNoReceipt).Unix())
	if err == nil {
		for _, id := range stale {
			s.closeStaleRun(ctx, id, "no_receipt", "script.lost")
		}
	}
	if seq, hash, err := s.store.ScriptChainHead(ctx); err == nil && seq > 0 {
		if b, err := json.Marshal(scriptHead{seq, hash}); err == nil {
			tmp := scriptHeadFile(dataDir) + ".tmp"
			if os.WriteFile(tmp, b, 0o600) == nil {
				_ = os.Rename(tmp, scriptHeadFile(dataDir))
			}
		}
		last, ok, err := s.store.LastScriptEvent(ctx, "anchor")
		if err == nil && (!ok || now.Unix()-last.Time >= 86400) {
			s.scriptAnchor(ctx)
			if _, err := s.store.PruneScriptLog(ctx, scriptLogKeep); err != nil {
				s.log.Error("betik kaydı saklama", "hata", err)
			}
			if err := s.store.CleanScriptOutputs(ctx, scriptOutKeep); err != nil {
				s.log.Error("betik çıktısı saklama", "hata", err)
			}
		}
	}
}

func (s *Server) closeStaleRun(ctx context.Context, runID, kind, msg string) {
	var req runRequest
	err := s.store.ScriptTx(ctx, func(l *store.ScriptLog) error {
		reqEv, e := l.Requested(ctx, runID)
		if e != nil {
			return e
		}
		if e := json.Unmarshal([]byte(reqEv.Data), &req); e != nil {
			return e
		}
		if e := l.Dequeue(ctx, runID); e != nil {
			return e
		}
		_, e = l.Append(ctx, runID, kind, 0, map[string]any{})
		return e
	})
	if err != nil {
		s.log.Error("betik işi kapatılamadı", "run", runID, "hata", err)
		return
	}
	s.notifyRun(ctx, req, runID, msg, req.ScriptID)
}
