package agentupdate

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Ajan tarafı: teklifi doğrula, programı indir, özet ve imzayı denetle,
// çalışan programı atomik olarak değiştir, yeniden başlat; yeni sürüm art
// arda çalışamazsa eskisine dön.
//
// Dosyalar programın yanında durur (ajanın yazabildiği tek yer burasıdır:
// Docker kurulumunda /opt/uptime birimi, systemd'de /usr/local/bin,
// Windows'ta %ProgramFiles%\Uptime):
//
//	uptime.new            indirilen program (doğrulanmadan önce; kalıntıysa silinir)
//	uptime.old            değişimden önceki program (geri dönüş için)
//	uptime.update         bekleyen güncelleme işareti (JSON: from, to, at, starts)
//	uptime.update-failed  geri dönüşün nedeni (eski program açılışta panele bildirir)

// ExitCode güncelleme sonrası çıkış kodu: süreç kendini yeniden başlatamaz;
// gözetmen (Docker --restart, systemd Restart=always, Windows hizmet kurtarma
// ayarı) yeni programla yeniden başlatır.
const ExitCode = 3

// ErrRestart ajan güncellendi (ya da geri alındı), gözetmenin yeniden
// başlatması için süreç ExitCode ile çıkmalı.
var ErrRestart = errors.New("program güncellendi, yeniden başlatılıyor")

// DefaultMaxSize indirilen programın üst sınırı (ajan ~40 MB).
const DefaultMaxSize = 200 << 20

// maxStarts yeni sürüm bu kadar başlatılıp panele hiç ulaşamazsa eskisine
// dönülür (işaret dosyasındaki sayaç; panele ulaşınca Confirm siler).
const maxStarts = 3

// Updater çalışan programı güncelleyen bileşen; nil ise ajan güncelleme
// tekliflerini yok sayar.
type Updater struct {
	Exe     string // çalışan programın yolu (symlink çözülmüş)
	Version string // çalışan sürüm
	OS      string // runtime.GOOS (testlerde farklı olabilir)
	Arch    string // runtime.GOARCH
	MaxSize int64  // 0: DefaultMaxSize
	Log     *slog.Logger
	// Disabled boş değilse güncelleme yapılmaz; neden panele bildirilir
	// ("off": AUTO_UPDATE=0, "readonly": program dizini yazılamıyor).
	Disabled string
	// now testlerde sabitlenir.
	now func() time.Time
}

// New çalışan program için güncelleyici hazırlar: yolu çözer ve program
// dizinine yazabildiğini denetler (yazamıyorsa Disabled="readonly": ör.
// uygulama imajındaki /usr/local/bin, uptime kullanıcısıyla).
func New(version string, log *slog.Logger) *Updater {
	u := &Updater{Version: version, OS: runtime.GOOS, Arch: runtime.GOARCH, Log: log}
	exe, err := os.Executable()
	if err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
	}
	if err != nil {
		u.Disabled = "readonly"
		log.Warn("otomatik güncelleme kapalı: program yolu bulunamadı", "hata", err)
		return u
	}
	u.Exe = exe
	if err := u.checkWritable(); err != nil {
		u.Disabled = "readonly"
		log.Info("otomatik güncelleme kapalı: program dizini yazılabilir değil", "dizin", filepath.Dir(exe), "hata", err)
	}
	return u
}

// checkWritable program dizinine dosya oluşturup silmeyi dener.
func (u *Updater) checkWritable() error {
	f, err := os.CreateTemp(filepath.Dir(u.Exe), ".uptime-yaz-*")
	if err != nil {
		return err
	}
	name := f.Name()
	f.Close()
	return os.Remove(name)
}

// Enabled güncelleme yapılabilir mi?
func (u *Updater) Enabled() bool { return u != nil && u.Disabled == "" && u.Exe != "" }

// Platform ajanın panele bildirdiği değer: "linux/amd64", kapalıysa
// "linux/amd64;off" ya da "linux/amd64;readonly".
func (u *Updater) Platform() string {
	if u == nil {
		return ""
	}
	p := u.OS + "/" + u.Arch
	if u.Disabled != "" {
		p += ";" + u.Disabled
	}
	return p
}

func (u *Updater) clock() time.Time {
	if u.now != nil {
		return u.now()
	}
	return time.Now()
}

func (u *Updater) logger() *slog.Logger {
	if u.Log == nil {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return u.Log
}

// Yardımcı dosya yolları.
func (u *Updater) newPath() string    { return u.Exe + ".new" }
func (u *Updater) oldPath() string    { return u.Exe + ".old" }
func (u *Updater) markerPath() string { return u.Exe + ".update" }
func (u *Updater) failedPath() string { return u.Exe + ".update-failed" }

// Check teklifin bu ajan için uygulanabilir ve imzasının geçerli olduğunu
// indirmeden önce denetler.
func (u *Updater) Check(o Offer) error {
	if !u.Enabled() {
		return errors.New("otomatik güncelleme bu ajanda kapalı")
	}
	if err := o.Manifest.Validate(); err != nil {
		return fmt.Errorf("teklif geçersiz: %w", err)
	}
	if o.OS != u.OS || o.Arch != u.Arch {
		return fmt.Errorf("teklif %s/%s için, bu ajan %s/%s", o.OS, o.Arch, u.OS, u.Arch)
	}
	if !IsVersion(u.Version) {
		return fmt.Errorf("çalışan sürüm (%s) sürüm numarası değil; yalnızca sürüm numaralı derlemeler güncellenir", u.Version)
	}
	if !Newer(u.Version, o.Version) {
		return fmt.Errorf("sunulan sürüm (%s) çalışan sürümden (%s) yeni değil", o.Version, u.Version)
	}
	if !strings.HasPrefix(o.URL, "/api/probe/binary") || strings.ContainsAny(o.URL, "\r\n \\") {
		return errors.New("teklifteki indirme adresi panelin program uç noktası değil")
	}
	pub, err := PublicKey()
	if err != nil {
		return err
	}
	if err := o.Signed.Verify(pub); err != nil {
		return fmt.Errorf("sürüm %s için imza geçersiz: %w", o.Version, err)
	}
	return nil
}

// Fetch indirmeyi yapar: gövdeyi w'ye yazar (ajan kendi kimlikli HTTP
// istemcisiyle panelden indirir).
type Fetch func(ctx context.Context, url string, w io.Writer) error

// Install teklifi uygular: Check, indirme (boyut sınırlı, özet hesaplanarak),
// SHA-256 denetimi, çalışan programın atomik değişimi (eski program .old
// olarak kalır) ve bekleyen güncelleme işareti. Başarıda çağıran süreci
// ExitCode ile bitirmelidir. Doğrulanmamış hiçbir şey çalıştırılmaz: dosya
// yalnızca tüm denetimlerden sonra yürütülebilir yapılıp yerine konur.
func (u *Updater) Install(ctx context.Context, o Offer, fetch Fetch) error {
	if err := u.Check(o); err != nil {
		return err
	}
	log := u.logger()
	tmp := u.newPath()
	os.Remove(tmp)
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("geçici dosya açılamadı: %w", err)
	}
	cleanup := func() { f.Close(); os.Remove(tmp) }
	limit := u.MaxSize
	if limit <= 0 {
		limit = DefaultMaxSize
	}
	h := sha256.New()
	lw := &limitedWriter{w: io.MultiWriter(f, h), left: limit}
	if err := fetch(ctx, o.URL, lw); err != nil {
		cleanup()
		if errors.Is(err, errTooLarge) {
			return fmt.Errorf("indirilen program %d MB sınırını aştı", limit>>20)
		}
		return fmt.Errorf("program indirilemedi: %w", err)
	}
	if lw.n == 0 {
		cleanup()
		return errors.New("indirilen program boş")
	}
	if err := f.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if sum := hex.EncodeToString(h.Sum(nil)); sum != o.SHA256 {
		os.Remove(tmp)
		return fmt.Errorf("indirilen programın SHA-256'sı imzalı bildirimle uyuşmuyor")
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := u.swap(tmp); err != nil {
		os.Remove(tmp)
		return err
	}
	m := marker{From: u.Version, To: o.Version, At: u.clock().Unix()}
	if err := writeJSON(u.markerPath(), m); err != nil {
		log.Warn("güncelleme işareti yazılamadı (geri dönüş izlenemez)", "hata", err)
	}
	log.Info("program güncellendi, yeniden başlatılıyor", "eski", u.Version, "yeni", o.Version, "boyut", lw.n)
	return nil
}

// swap doğrulanmış yeni programı yerine koyar: çalışan program .old adına
// alınır (çalışan bir dosya Windows'ta da yeniden adlandırılabilir; silinemez),
// yeni program onun adını alır. İkinci adım başarısız olursa eski geri gelir.
func (u *Updater) swap(tmp string) error {
	old := u.oldPath()
	os.Remove(old) // önceki güncellemeden kalan
	if err := os.Rename(u.Exe, old); err != nil {
		return fmt.Errorf("çalışan program kenara alınamadı: %w", err)
	}
	if err := os.Rename(tmp, u.Exe); err != nil {
		os.Rename(old, u.Exe) // geri al
		return fmt.Errorf("yeni program yerine konamadı: %w", err)
	}
	syncDir(filepath.Dir(u.Exe))
	return nil
}

// marker bekleyen güncelleme işareti.
type marker struct {
	From   string `json:"from"`
	To     string `json:"to"`
	At     int64  `json:"at"`
	Starts int    `json:"starts"` // yeni sürümün kaç kez başlatıldığı
}

// failure geri dönüş/başarısızlık notu: eski program açılışta panele bildirir.
type failure struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Status string `json:"status"` // StatusRolledBack | StatusFailed
	Error  string `json:"error"`
	At     int64  `json:"at"`
}

// Startup ajan başlarken çağrılır. Bekleyen güncelleme işaretine bakar:
//
//   - çalışan sürüm işaretin hedefiyse başlatma sayacını artırır; sayaç
//     sınırı aşarsa yeni program çalışamıyor demektir: .old geri getirilir,
//     neden not edilir ve ErrRestart döner (süreç ExitCode ile çıkar, gözetmen
//     eski programı başlatır);
//   - çalışan sürüm işaretin kaynağıysa (eski program yeniden çalışıyor)
//     değişim bir şekilde geri gitmiş demektir: başarısızlık notu yazılır;
//   - ikisi de değilse işaret eskidir, silinir.
//
// Kalıntı .new dosyası her durumda silinir. Panele bildirilecek bir not varsa
// döner (Report); çağıran panele ulaşınca Confirm ile siler.
func (u *Updater) Startup() (*Report, error) {
	if u == nil || u.Exe == "" {
		return nil, nil
	}
	log := u.logger()
	os.Remove(u.newPath())
	var m marker
	if err := readJSON(u.markerPath(), &m); err == nil {
		switch u.Version {
		case m.To:
			m.Starts++
			if m.Starts > maxStarts {
				reason := fmt.Sprintf("yeni sürüm %s %d kez başlatıldı ama panele ulaşamadı", m.To, m.Starts-1)
				log.Error("güncelleme geri alınıyor", "neden", reason)
				if err := u.rollback(); err != nil {
					log.Error("eski program geri getirilemedi", "hata", err)
					// Geri dönülemiyor: yeni sürümle devam etmekten başka çare yok.
					os.Remove(u.markerPath())
					return &Report{Status: StatusFailed, Version: m.To, Error: reason + "; eski program geri getirilemedi: " + err.Error()}, nil
				}
				writeJSON(u.failedPath(), failure{From: m.From, To: m.To, Status: StatusRolledBack, Error: reason, At: u.clock().Unix()})
				os.Remove(u.markerPath())
				return nil, ErrRestart
			}
			writeJSON(u.markerPath(), m)
			log.Info("güncellenmiş sürüm başlıyor", "eski", m.From, "yeni", m.To, "deneme", m.Starts)
		case m.From:
			os.Remove(u.markerPath())
			writeJSON(u.failedPath(), failure{From: m.From, To: m.To, Status: StatusFailed,
				Error: "yeni program yerine konmuş görünüyor ama eski sürüm çalışıyor", At: u.clock().Unix()})
		default:
			os.Remove(u.markerPath())
		}
	}
	var f failure
	if err := readJSON(u.failedPath(), &f); err == nil && f.To != "" {
		status := f.Status
		if status == "" {
			status = StatusFailed
		}
		return &Report{Status: status, Version: f.To, Error: f.Error}, nil
	}
	return nil, nil
}

// rollback .old programını yerine getirir; çalışamayan yeni program
// .failed adıyla kalır (inceleme için; sonraki güncellemede silinir).
func (u *Updater) rollback() error {
	old := u.oldPath()
	if _, err := os.Stat(old); err != nil {
		return fmt.Errorf("eski program yok: %w", err)
	}
	failed := u.Exe + ".failed"
	os.Remove(failed)
	if err := os.Rename(u.Exe, failed); err != nil {
		return err
	}
	if err := os.Rename(old, u.Exe); err != nil {
		os.Rename(failed, u.Exe)
		return err
	}
	syncDir(filepath.Dir(u.Exe))
	return nil
}

// Confirm ajan panele ulaştı: bekleyen güncelleme başarılı sayılır, işaret
// ve başarısızlık notu silinir, eski program kaldırılır (Windows'ta eski
// süreç bittiği için artık silinebilir).
func (u *Updater) Confirm() {
	if u == nil || u.Exe == "" {
		return
	}
	var m marker
	if err := readJSON(u.markerPath(), &m); err == nil && m.To == u.Version {
		u.logger().Info("güncelleme tamamlandı", "eski", m.From, "yeni", m.To)
		os.Remove(u.oldPath())
		os.Remove(u.Exe + ".failed")
	}
	os.Remove(u.markerPath())
	os.Remove(u.failedPath())
}

// Dosya yardımcıları ----------------------------------------------------------------------

var errTooLarge = errors.New("boyut sınırı aşıldı")

// limitedWriter sınırı aşan yazmayı hata ile keser (indirme sınırı).
type limitedWriter struct {
	w    io.Writer
	left int64
	n    int64
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > l.left {
		return 0, errTooLarge
	}
	n, err := l.w.Write(p)
	l.left -= int64(n)
	l.n += int64(n)
	return n, err
}

// writeJSON küçük bir JSON dosyasını geçici ad üzerinden atomik yazar.
func writeJSON(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// syncDir dizin girdilerini (yeniden adlandırmalar) diske yazdırır (en iyi
// çaba; Windows'ta dizin açılamaz, yok sayılır).
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	d.Sync()
	d.Close()
}

// KeyFromPEM PKCS#8 (PEM) Ed25519 özel anahtarını çözer (cmd/ajanimza; testler).
func KeyFromPEM(data []byte) (ed25519.PrivateKey, error) {
	return parsePKCS8(data)
}
