package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/kadirsa1105/uptime-kadir-app/internal/backup"
	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
)

var errUploadTooLarge = errors.New("dosya çok büyük")

// uptimeRobotClient testlerde değiştirilebilir.
var uptimeRobotClient = &http.Client{Timeout: 30 * time.Second}

// extendDeadlines büyük dosya yüklemesi sunucunun genel 30 sn okuma sınırına takılmasın.
func extendDeadlines(w http.ResponseWriter) {
	rc := http.NewResponseController(w)
	rc.SetReadDeadline(time.Now().Add(importUploadDeadline))
	rc.SetWriteDeadline(time.Now().Add(importUploadDeadline))
}

// readUpload yüklenen dosyayı dst'ye kopyalar: multipart/form-data ise "file"
// alanı, değilse istek gövdesinin kendisi. limit aşılırsa errUploadTooLarge.
func readUpload(r *http.Request, limit int64, dst io.Writer) error {
	src := io.Reader(r.Body)
	if ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); ct == "multipart/form-data" {
		mr, err := r.MultipartReader()
		if err != nil {
			return importInputError("Dosya okunamadı: " + err.Error())
		}
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				return importInputError("İstekte “file” alanında bir dosya yok")
			}
			if err != nil {
				return importInputError("Dosya okunamadı: " + err.Error())
			}
			if part.FormName() == "file" {
				src = part
				break
			}
			part.Close()
		}
	}
	n, err := io.Copy(dst, io.LimitReader(src, limit+1))
	if err != nil {
		return importInputError("Dosya okunamadı: " + err.Error())
	}
	if n > limit {
		return errUploadTooLarge
	}
	if n == 0 {
		return importInputError("Dosya boş")
	}
	return nil
}

func (s *Server) writeImportError(w http.ResponseWriter, err error, limitMB int) {
	var ie importInputError
	switch {
	case errors.Is(err, errUploadTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("Dosya çok büyük (en fazla %d MB)", limitMB))
	case errors.As(err, &ie):
		writeError(w, http.StatusBadRequest, ie.Error())
	default:
		s.dbError(w, err)
	}
}

func dryRun(r *http.Request) bool {
	v := r.URL.Query().Get("dry_run")
	return v == "1" || v == "true"
}

// importBackup: POST /api/import?mode=merge|replace[&confirm=yes][&dry_run=1]
// Gövde: bu uygulamanın yedek dosyası (JSON, en fazla 20 MB; doğrudan gövde
// veya multipart "file" alanı).
func (s *Server) importBackup(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	mode := q.Get("mode")
	if mode == "" {
		mode = "merge"
	}
	if mode != "merge" && mode != "replace" {
		writeError(w, http.StatusBadRequest, "Mod merge (birleştir) veya replace (değiştir) olmalı")
		return
	}
	if mode == "replace" && q.Get("confirm") != "yes" && !dryRun(r) {
		writeError(w, http.StatusBadRequest, "Değiştir modu mevcut tüm monitörleri, bildirim kanallarını, etiketleri ve durum sayfalarını siler; onay için confirm=yes gönderin")
		return
	}
	extendDeadlines(w)
	var buf strings.Builder
	if err := readUpload(r, maxBackupBytes, &buf); err != nil {
		s.writeImportError(w, err, maxBackupBytes>>20)
		return
	}
	data := []byte(buf.String())
	var probe struct {
		Format      string          `json:"format"`
		MonitorList json.RawMessage `json:"monitorList"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		writeError(w, http.StatusBadRequest, "Yedek dosyası okunamadı: geçerli bir JSON nesnesi değil")
		return
	}
	if probe.Format != backup.Format {
		msg := "Bu dosya bir Uptime yedeği değil"
		if probe.MonitorList != nil {
			msg += "; Uptime Kuma yedeği için “Uptime Kuma'dan içe aktar”ı kullanın"
		}
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	var doc backup.Doc
	if err := json.Unmarshal(data, &doc); err != nil {
		writeError(w, http.StatusBadRequest, "Yedek dosyası okunamadı: "+err.Error())
		return
	}
	if doc.Version < 1 || doc.Version > backup.Version {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Yedek sürümü (%d) desteklenmiyor; uygulamayı güncelleyin", doc.Version))
		return
	}
	s.runImport(w, r, "uptime-kadir", &backup.Result{Doc: &doc}, mode == "replace")
}

// importKuma: POST /api/import/uptime-kuma[?dry_run=1] — Uptime Kuma JSON
// yedeği (en fazla 20 MB) veya kuma.db (en fazla 200 MB); multipart "file"
// alanında ya da doğrudan gövdede. Her zaman birleştirme modunda çalışır.
func (s *Server) importKuma(w http.ResponseWriter, r *http.Request) {
	extendDeadlines(w)
	f, err := os.CreateTemp("", "kuma-ice-aktar-*.db")
	if err != nil {
		s.dbError(w, err)
		return
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := readUpload(r, maxKumaUploadBytes, f); err != nil {
		s.writeImportError(w, err, maxKumaUploadBytes>>20)
		return
	}
	head := make([]byte, 16)
	n, _ := f.ReadAt(head, 0)
	var res *backup.Result
	if backup.IsSQLite(head[:n]) {
		if err := f.Sync(); err != nil {
			s.dbError(w, err)
			return
		}
		res, err = backup.FromKumaDB(r.Context(), f.Name())
	} else {
		st, _ := f.Stat()
		if st.Size() > maxBackupBytes {
			writeError(w, http.StatusRequestEntityTooLarge, "JSON yedeği en fazla 20 MB olabilir")
			return
		}
		data := make([]byte, st.Size())
		if _, err := f.ReadAt(data, 0); err != nil && err != io.EOF {
			s.dbError(w, err)
			return
		}
		if !json.Valid(data) {
			writeError(w, http.StatusBadRequest, "Dosya tanınmadı: Uptime Kuma JSON yedeği veya kuma.db olmalı")
			return
		}
		res, err = backup.FromKumaJSON(data)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.runImport(w, r, "uptime-kuma", res, false)
}

// importUptimeRobot: POST /api/import/uptimerobot[?dry_run=1], gövde {"api_key": "..."}.
func (s *Server) importUptimeRobot(w http.ResponseWriter, r *http.Request) {
	var in struct {
		APIKey string `json:"api_key"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	key := strings.TrimSpace(in.APIKey)
	if key == "" || len(key) > 200 || strings.ContainsAny(key, " \t\r\n") {
		writeError(w, http.StatusBadRequest, "Geçerli bir UptimeRobot API anahtarı girin")
		return
	}
	list, err := backup.FetchUptimeRobot(r.Context(), uptimeRobotClient, key)
	var ue *backup.ErrUptimeRobot
	switch {
	case errors.As(err, &ue):
		writeError(w, http.StatusBadRequest, ue.Error())
		return
	case err != nil:
		s.log.Warn("UptimeRobot'a bağlanılamadı", "hata", err)
		writeError(w, http.StatusUnprocessableEntity, err.Error()) // 502 proxy'de yutulabilir
		return
	}
	s.runImport(w, r, "uptimerobot", backup.FromUptimeRobot(list), false)
}

// runImport planı hazırlar; deneme değilse tek işlemde yazar, monitörleri başlatır.
func (s *Server) runImport(w http.ResponseWriter, r *http.Request, source string, conv *backup.Result, replace bool) {
	if !importMu.TryLock() {
		writeError(w, http.StatusConflict, "Başka bir içe aktarma sürüyor; bitmesini bekleyin")
		return
	}
	defer importMu.Unlock()
	ctx := r.Context()
	pl, err := s.planImport(ctx, conv, replace, requestHost(r))
	if err != nil {
		s.writeImportError(w, err, 0)
		return
	}
	sum := &pl.sum
	sum.Source, sum.Mode, sum.DryRun = source, "merge", dryRun(r)
	var old []store.Monitor
	if replace {
		sum.Mode = "replace"
		c, err := s.store.CountForImport(ctx)
		if err != nil {
			s.dbError(w, err)
			return
		}
		sum.Deleted = &importCounts{Monitors: c.Monitors, Notifications: c.Notifications, Tags: c.Tags, StatusPages: c.StatusPages}
		if old, err = s.store.ListMonitors(ctx); err != nil {
			s.dbError(w, err)
			return
		}
	}
	if sum.DryRun {
		writeJSON(w, http.StatusOK, sum)
		return
	}

	// Değiştir modunda eski monitörlerin kontrolleri durdurulur; yazma başarısız
	// olursa (işlem geri alınır) yeniden başlatılır.
	for _, m := range old {
		s.engine.Remove(m.ID)
	}
	res, err := s.store.Import(ctx, &pl.data)
	if err != nil {
		for _, m := range old {
			s.engine.Reload(ctx, m.ID)
		}
		if store.IsUniqueViolation(err) {
			writeError(w, http.StatusConflict, "İçe aktarma başarısız: çakışan kayıt var (push adresi, durum sayfası adresi veya alan adı); hiçbir değişiklik yapılmadı")
			return
		}
		s.dbError(w, err)
		return
	}
	for i, id := range res.MonitorIDs {
		sum.Items[pl.monitorItems[i]].ID = id
		if err := s.engine.Reload(ctx, id); err != nil {
			s.log.Error("içe aktarılan monitör başlatılamadı", "id", id, "hata", err)
		}
	}
	if pl.data.Settings != nil {
		s.engine.SetSettings(*pl.data.Settings)
	}
	// Durum sayfası önbelleği/özel alan adları ve bakım dizini (değiştir modunda
	// silinen monitörler) yeni verilere göre yenilenir.
	s.pagesChanged(ctx)
	if err := s.engine.ReloadMaintenance(ctx); err != nil {
		s.log.Error("bakım pencereleri yenilenemedi", "hata", err)
	}
	detail := fmt.Sprintf("kaynak=%s mod=%s eklenen: %d monitör, %d bildirim, %d etiket, %d durum sayfası; atlanan: %d",
		source, sum.Mode, sum.Created.Monitors, sum.Created.Notifications, sum.Created.Tags, sum.Created.StatusPages,
		sum.Skipped.Monitors+sum.Skipped.Notifications+sum.Skipped.Tags+sum.Skipped.StatusPages)
	s.audit(r, store.User{}, "backup.import", "backup", 0, source, detail)
	s.log.Info("içe aktarma tamamlandı", "kaynak", source, "mod", sum.Mode, "monitör", sum.Created.Monitors)
	writeJSON(w, http.StatusOK, sum)
}
