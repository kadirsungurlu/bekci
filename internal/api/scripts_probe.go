package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/kadirsungurlu/bekci/internal/betik"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// Ajan tarafı: yetenek başlığı, iş iletimi, olay alımı (docs/BETIK-SOZLESME.md §3-5).

func init() {
	RegisterRoutes(func(s *Server, mux *http.ServeMux) {
		mux.Handle("POST /api/probe/script-result", s.probeOnly(s.probeScriptResult))
	})
}

type capability struct {
	schemas, state, reason string
}

// parseCapability X-Probe-Scripts; çözülemeyen başlık state=off sayılır.
func parseCapability(h string) capability {
	c := capability{state: "off", reason: "config"}
	for _, f := range strings.Split(h, ";") {
		k, v, _ := strings.Cut(strings.TrimSpace(f), "=")
		switch k {
		case "schemas":
			if len(v) <= 32 {
				c.schemas = v
			}
		case "state":
			if v == "on" {
				c.state, c.reason = "on", ""
			}
		case "reason":
			if c.state != "on" && reasonRe.MatchString(v) {
				c.reason = v
			}
		}
	}
	return c
}

var reasonRe = regexp.MustCompile(`^[a-z_]{1,32}$`)

// scriptObserve yetenek başlığını kaydeder. Yazma seyrektir: değişince ya da 5
// dakikada bir. Yeni anahtar otomatik kabul edilmez, bekleyen olur.
func (s *Server) scriptObserve(ctx context.Context, r *http.Request, p store.Probe) (capability, bool) {
	h := r.Header.Get("X-Probe-Scripts")
	if h == "" || !s.store.Postgres() {
		return capability{}, false
	}
	c := parseCapability(h)
	var keyID, pub string
	if c.state == "on" {
		pk, err := betik.ParsePublicKey(r.Header.Get("X-Probe-Script-Key"))
		if err != nil {
			c = capability{schemas: c.schemas, state: "off", reason: "state"}
		} else {
			keyID, pub = betik.KeyID(pk), betik.EncodePublicKey(pk)
		}
	}
	sig := c.schemas + "|" + c.state + "|" + c.reason + "|" + keyID
	now := s.now().Unix()
	if v, ok := s.scriptSeen.Load(p.ID); ok {
		if e := v.(scriptSeenEntry); e.sig == sig && now-e.at < 300 {
			return c, true
		}
	}
	changed, err := s.store.ObserveScriptHost(ctx, p.ID, keyID, pub, c.schemas, c.state, c.reason, now)
	if err != nil {
		s.log.Error("betik yetenek kaydı", "sunucu", p.ID, "hata", err)
		return c, true
	}
	s.scriptSeen.Store(p.ID, scriptSeenEntry{sig, now})
	if changed {
		err := s.store.ScriptTx(ctx, func(l *store.ScriptLog) error {
			_, e := l.Append(ctx, "", "host_key_seen", 0, map[string]any{"probe_id": p.ID, "host_key": keyID, "public_key": pub})
			return e
		})
		if err != nil {
			s.log.Error("betik anahtar olayı", "hata", err)
		}
		s.scriptNotify(ctx, p, "key-"+keyID[:16], "script.host_key", keyID[:16])
	}
	return c, true
}

type scriptSeenEntry struct {
	sig string
	at  int64
}

// scriptJobsFor yoklama yanıtının script_jobs alanı. Başlıksız (eski) ya da
// kapalı ajana hiçbir şey gönderilmez (§10): nil döner, alan yanıta girmez.
func (s *Server) scriptJobsFor(ctx context.Context, r *http.Request, p store.Probe) *betik.JobList {
	c, ok := s.scriptObserve(ctx, r, p)
	if !ok || c.state != "on" {
		return nil
	}
	ctl, err := s.store.ScriptControlState(ctx)
	if err != nil {
		s.log.Error("betik durumu", "hata", err)
		return nil
	}
	var out betik.JobList
	revokes, err := s.store.ListScriptRevokes(ctx)
	if err != nil {
		s.log.Error("betik iptal listesi", "hata", err)
		return nil
	}
	seen := map[string]bool{}
	for _, rv := range revokes { // liste azalan sürümde: betik başına en yükseği yeter
		if !seen[rv.ScriptID] && len(out.Revokes) < 64 {
			seen[rv.ScriptID] = true
			out.Revokes = append(out.Revokes, json.RawMessage(rv.Envelope))
		}
	}
	jobs, err := s.store.ScriptJobsFor(ctx, p.ID, s.now().Unix())
	if err != nil {
		s.log.Error("betik işleri", "hata", err)
		return nil
	}
	for _, j := range jobs {
		if j.Cancel {
			out.Cancel = append(out.Cancel, j.RunID)
			continue
		}
		if ctl.Stopped || j.DeliveredAt != 0 {
			continue
		}
		row, err := s.store.GetScript(ctx, j.ScriptID, j.Version)
		if err != nil {
			s.log.Error("betik okunamadı", "betik", j.ScriptID, "hata", err)
			continue
		}
		job := betik.RunJob{RunID: j.RunID, Manifest: json.RawMessage(row.Envelope), Body: row.Body, Params: []byte(j.Params)}
		if j.Ticket != "" {
			job.Ticket = json.RawMessage(j.Ticket)
		}
		out.Runs = append(out.Runs, job)
	}
	if len(out.Runs) == 0 && len(out.Revokes) == 0 && len(out.Cancel) == 0 {
		return nil
	}
	return &out
}

var (
	rejectCodeRe = regexp.MustCompile(`^[a-z_]{1,32}$`)
	ansiRe       = regexp.MustCompile("\x1b(\\[[0-?]*[ -/]*[@-~]|\\][^\x07\x1b]*(\x07|\x1b\\\\)?|[@-Z\\\\-_])")
)

// cleanOutput ajan çıktısını metne indirger (A3): ANSI kaçışları ve denetim
// karakterleri atılır, geçersiz UTF-8 onarılır. Arayüz yine yalnız metin gösterir.
func cleanOutput(b []byte) []byte {
	b = ansiRe.ReplaceAll(b, nil)
	out := make([]byte, 0, len(b))
	for _, r := range strings.ToValidUTF8(string(b), "�") {
		if r == '\n' || r == '\t' || (r >= 0x20 && r != 0x7f && !(r >= 0x80 && r < 0xa0)) {
			out = utf8.AppendRune(out, r)
		}
	}
	return out
}

// probeScriptResult ajan olayı. Yanıt: 2xx kabul; 409/422 kalıcı red (sözleşme §5).
func (s *Server) probeScriptResult(w http.ResponseWriter, r *http.Request) {
	if !s.store.Postgres() {
		writeError(w, http.StatusNotFound, "Bulunamadı")
		return
	}
	p := probeFrom(r)
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "Gövde çok büyük")
		return
	}
	var ev betik.Event
	if err := json.Unmarshal(raw, &ev); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "Geçersiz olay")
		return
	}
	ctx := r.Context()
	if ev.Kind == "notice" {
		if !rejectCodeRe.MatchString(ev.Code) {
			writeError(w, http.StatusUnprocessableEntity, "Geçersiz kod")
			return
		}
		err := s.store.ScriptTx(ctx, func(l *store.ScriptLog) error {
			_, e := l.Append(ctx, "", "notice", 0, map[string]any{"probe_id": p.ID, "code": ev.Code, "agent_time": ev.Time})
			return e
		})
		if err != nil {
			s.dbError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	if ev.Kind == "rejected" && ev.RunID == "" { // biçimi bozuk run_id
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	if !betik.IsRunID(ev.RunID) {
		writeError(w, http.StatusUnprocessableEntity, "Geçersiz run_id")
		return
	}
	var note func()
	status := http.StatusOK
	err = s.store.ScriptTx(ctx, func(l *store.ScriptLog) error {
		reqEv, e := l.Requested(ctx, ev.RunID)
		if errors.Is(e, sql.ErrNoRows) {
			status = http.StatusConflict
			return nil
		}
		if e != nil {
			return e
		}
		var req runRequest
		if e := json.Unmarshal([]byte(reqEv.Data), &req); e != nil {
			return e
		}
		if req.ProbeID != p.ID { // A2/A5: run_id ajana bağlıdır
			status = http.StatusConflict
			return nil
		}
		last, e := l.LastKind(ctx, ev.RunID)
		if e != nil {
			return e
		}
		state, _, _ := runStateOf(req, last)
		switch ev.Kind {
		case "accepted":
			if state != runQueued {
				return nil
			}
			if _, e := l.MarkDelivered(ctx, ev.RunID, s.now().Unix()); e != nil {
				return e
			}
			_, e := l.Append(ctx, ev.RunID, "accepted", 0, map[string]any{"agent_time": ev.Time})
			return e
		case "rejected":
			if !rejectCodeRe.MatchString(ev.Code) {
				status = http.StatusUnprocessableEntity
				return nil
			}
			if state != runQueued { // accepted görülmüş işte run_id_replay yok sayılır
				return nil
			}
			if e := l.Dequeue(ctx, ev.RunID); e != nil {
				return e
			}
			if _, e := l.Append(ctx, ev.RunID, "agent_rejected", 0, map[string]any{"code": ev.Code, "agent_time": ev.Time}); e != nil {
				return e
			}
			note = func() { s.notifyRun(ctx, req, ev.RunID, "script.agent_rejected", req.ScriptID, ev.Code) }
			return nil
		case "receipt":
			if last.Kind == "receipt" { // aynı makbuz ikinci kez
				return nil
			}
			switch state {
			case runQueued, runAccepted, runCancelling, runCancelled, runExpired, runLost:
			default:
				status = http.StatusConflict
				return nil
			}
			return s.takeReceipt(ctx, l, p, ev, req, &status, &note)
		}
		status = http.StatusUnprocessableEntity
		return nil
	})
	if err != nil {
		s.dbError(w, err)
		return
	}
	if note != nil {
		note()
	}
	if status != http.StatusOK {
		writeError(w, status, "Olay reddedildi")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// takeReceipt makbuzu ajanın onaylı anahtarıyla doğrular ve kayda geçirir. Sıra
// boşluğu reddedilmez (kuyruk düşmüş olabilir), işaretlenir ve bildirilir.
func (s *Server) takeReceipt(ctx context.Context, l *store.ScriptLog, p store.Probe, ev betik.Event, req runRequest, status *int, note *func()) error {
	h, err := l.HostForReceipt(ctx, p.ID)
	if err != nil {
		return err
	}
	pub, err := betik.ParsePublicKey(h.PublicKey)
	if err != nil {
		*status = http.StatusConflict
		return nil
	}
	env, err := betik.ParseEnvelope(ev.Receipt)
	if err != nil || !env.Verify(betik.KindReceipt, pub) {
		*status = http.StatusUnprocessableEntity
		*note = func() { s.notifyRun(ctx, req, ev.RunID, "script.receipt_bad", req.ScriptID) }
		return nil
	}
	rc, err := betik.ParseReceipt(env.Payload)
	if err != nil || rc.RunID != ev.RunID || rc.ScriptID != req.ScriptID || rc.Version != req.Version ||
		rc.ManifestSHA256 != req.ManifestSHA256 || rc.ParamsSHA256 != req.ParamsSHA256 {
		*status = http.StatusUnprocessableEntity
		*note = func() { s.notifyRun(ctx, req, ev.RunID, "script.receipt_bad", req.ScriptID) }
		return nil
	}
	gap := rc.Seq != h.ReceiptSeq+1 || rc.Prev != h.ReceiptHash
	if rc.Seq > h.ReceiptSeq {
		if err := l.SetReceiptHead(ctx, p.ID, rc.Seq, env.PayloadDigest()); err != nil {
			return err
		}
	}
	if err := l.Dequeue(ctx, ev.RunID); err != nil {
		return err
	}
	if len(ev.Output) > 0 {
		if err := l.SaveOutput(ctx, ev.RunID, cleanOutput(ev.Output), rc.OutputSHA256, rc.OutputTruncated, s.now().Unix()); err != nil {
			return err
		}
	}
	if _, err := l.Append(ctx, ev.RunID, "receipt", 0, map[string]any{"receipt": json.RawMessage(ev.Receipt), "status": rc.Status,
		"exit_code": rc.ExitCode, "seq": rc.Seq, "gap": gap, "started_at": rc.StartedAt, "ended_at": rc.EndedAt, "output_sha256": rc.OutputSHA256}); err != nil {
		return err
	}
	*note = func() {
		s.notifyRun(ctx, req, ev.RunID, "script.finished", req.ScriptID, rc.Status, rc.ExitCode)
		if gap {
			s.notifyRun(ctx, req, ev.RunID+"-gap", "script.receipt_gap", req.ScriptID, rc.Seq, h.ReceiptSeq)
		}
	}
	return nil
}
