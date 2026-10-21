package api

// Yeni Bekci sürümü bildirimi. Panel açılışta bir kez ve 6 saatte bir GitHub'daki
// sürümleri (yayınlar + etiketler) okur; kurulu sürümden yenisi varsa yönetici
// panelde ince bir şerit görür. Kullanıcı şeridi o sürüm için kapatabilir; daha
// yeni sürüm çıkınca yeniden görünür. Ayarlardan ya da UPDATE_CHECK=off ile
// tamamen kapatılır: kapalıyken dışarıya hiç istek gitmez. Denetim hatası
// sessizdir (günlüğe tek satır) ve istek yolunu beklemez.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

func init() {
	RegisterRoutes(func(s *Server, mux *http.ServeMux) {
		mux.Handle("GET /api/update", s.admin(s.updateStatus))
		mux.Handle("POST /api/update/check", s.admin(s.updateCheckNow))
		mux.Handle("POST /api/update/dismiss", s.admin(s.updateDismiss))
	})
}

const (
	// DefaultUpdateCheckURL GitHub API'de deponun kökü (+ /releases, /tags).
	DefaultUpdateCheckURL = "https://api.github.com/repos/kadirsungurlu/bekci"
	updateRepoWeb         = "https://github.com/kadirsungurlu/bekci"
	updateInterval        = 6 * time.Hour
)

type updateState struct {
	url    string
	envOff bool // UPDATE_CHECK=off: ayardan bağımsız kapalı

	mu       sync.Mutex
	latest   string // en yeni sürüm (v öneksiz), "" = bilinmiyor/yok
	notes    string
	checked  int64
	checking sync.Mutex // aynı anda tek denetim
}

// ConfigureUpdateCheck denetim adresini ve ortam değişkeniyle kapatmayı ayarlar.
func (s *Server) ConfigureUpdateCheck(url string, off bool) {
	if url == "" {
		url = DefaultUpdateCheckURL
	}
	s.upd.url, s.upd.envOff = strings.TrimRight(url, "/"), off
}

// UpdateCheckOffEnv UPDATE_CHECK değerinin kapatma anlamına gelip gelmediği.
func UpdateCheckOffEnv(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "0", "off", "false", "no", "kapali", "hayir":
		return true
	}
	return false
}

// updateEnabled denetim açık mı (ortam değişkeni, ayar, sürümü bilinen derleme).
func (s *Server) updateEnabled(ctx context.Context) bool {
	if s.upd.envOff || s.upd.url == "" {
		return false
	}
	if _, ok := parseSemver(s.version); !ok {
		return false // "dev" gibi yerel derleme
	}
	st, err := s.store.LoadSettings(ctx)
	return err == nil && st.UpdateCheckOn()
}

// RunUpdateCheck açılışta bir kez (kısa gecikmeyle) ve 6 saatte bir denetler.
func (s *Server) RunUpdateCheck(ctx context.Context) {
	t := time.NewTimer(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		s.checkUpdate(ctx)
		t.Reset(updateInterval)
	}
}

// checkUpdate denetim kapalıysa hiçbir şey yapmaz; hata günlüğe tek satır yazılır.
func (s *Server) checkUpdate(ctx context.Context) {
	if !s.updateEnabled(ctx) {
		return
	}
	s.upd.checking.Lock()
	defer s.upd.checking.Unlock()
	cur, _ := parseSemver(s.version)
	latest, notes, err := fetchLatest(ctx, s.upd.url, cur.pre != nil)
	if err != nil {
		s.log.Info("yeni sürüm denetlenemedi", "hata", err)
		return
	}
	s.upd.mu.Lock()
	s.upd.latest, s.upd.notes, s.upd.checked = latest, notes, s.now().Unix()
	s.upd.mu.Unlock()
}

type ghRelease struct {
	Tag        string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	URL        string `json:"html_url"`
}

// fetchLatest yayınlar ve etiketlerden en yeni sürümü seçer; ön sürümler
// yalnız kurulu sürüm de ön sürümse hesaba katılır.
func fetchLatest(ctx context.Context, base string, withPre bool) (latest, notes string, err error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var rels []ghRelease
	var tags []struct {
		Name string `json:"name"`
	}
	errR := getJSON(ctx, base+"/releases?per_page=30", &rels)
	errT := getJSON(ctx, base+"/tags?per_page=100", &tags)
	if errR != nil && errT != nil {
		return "", "", errR
	}
	urls := map[string]string{}
	var best semver
	found := false
	consider := func(tag, url string) {
		v, ok := parseSemver(tag)
		if !ok || (v.pre != nil && !withPre) {
			return
		}
		if url != "" {
			urls[v.String()] = url
		}
		if !found || v.compare(best) > 0 {
			best, found = v, true
		}
	}
	for _, r := range rels {
		if !r.Draft {
			consider(r.Tag, r.URL)
		}
	}
	for _, t := range tags {
		consider(t.Name, "")
	}
	if !found {
		return "", "", nil
	}
	latest = best.String()
	notes = urls[latest]
	if notes == "" {
		notes = updateRepoWeb + "/releases/tag/v" + latest
	}
	return latest, notes, nil
}

func getJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "bekci-update-check")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(out)
}

// Uçlar ----------------------------------------------------------------------------

type updateView struct {
	Enabled   bool   `json:"enabled"`
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Newer     bool   `json:"newer"`
	NotesURL  string `json:"notes_url"`
	Dismissed string `json:"dismissed"`
	Show      bool   `json:"show"` // şerit gösterilsin
	CheckedAt int64  `json:"checked_at"`
}

func (s *Server) updateView(r *http.Request) (updateView, error) {
	v := updateView{Current: s.version, Enabled: s.updateEnabled(r.Context())}
	s.upd.mu.Lock()
	v.Latest, v.NotesURL, v.CheckedAt = s.upd.latest, s.upd.notes, s.upd.checked
	s.upd.mu.Unlock()
	if !v.Enabled {
		return v, nil
	}
	cur, _ := parseSemver(s.version)
	lat, ok := parseSemver(v.Latest)
	v.Newer = ok && lat.compare(cur) > 0
	d, err := s.store.UpdateDismissed(r.Context(), userFrom(r).ID)
	if err != nil {
		return v, err
	}
	v.Dismissed = d
	dv, dok := parseSemver(d)
	v.Show = v.Newer && (!dok || lat.compare(dv) > 0)
	return v, nil
}

func (s *Server) updateStatus(w http.ResponseWriter, r *http.Request) {
	v, err := s.updateView(r)
	if err != nil {
		s.dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// updateCheckNow şimdi denetler (ayarlardaki düğme); kapalıysa istek atılmaz.
func (s *Server) updateCheckNow(w http.ResponseWriter, r *http.Request) {
	s.checkUpdate(r.Context())
	s.updateStatus(w, r)
}

func (s *Server) updateDismiss(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Version string `json:"version"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	v, ok := parseSemver(in.Version)
	if !ok {
		writeError(w, http.StatusBadRequest, "Geçersiz sürüm")
		return
	}
	if err := s.store.DismissUpdate(r.Context(), userFrom(r).ID, v.String()); err != nil {
		s.dbError(w, err)
		return
	}
	s.updateStatus(w, r)
}

// Sürüm karşılaştırma (semver 2.0) -----------------------------------------------

type semver struct {
	major, minor, patch int
	pre                 []string // nil: kararlı sürüm
}

// imageFlavors sürüm sonundaki imaj türü ekleri (ön sürüm değildir).
var imageFlavors = []string{"-postgres"}

// parseSemver "v1.2.3", "1.2.3-rc.1", "1.2.3-postgres", "1.2.3-rc.1-postgres"
// biçimlerini okur; derleme üst verisi (+...) ve imaj türü eki yok sayılır.
func parseSemver(s string) (semver, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	for _, f := range imageFlavors {
		s = strings.TrimSuffix(s, f)
	}
	core, pre, hasPre := strings.Cut(s, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	var n [3]int
	for i, p := range parts {
		x, err := strconv.Atoi(p)
		if err != nil || x < 0 || p == "" || (len(p) > 1 && p[0] == '0') {
			return semver{}, false
		}
		n[i] = x
	}
	v := semver{major: n[0], minor: n[1], patch: n[2]}
	if hasPre {
		if pre == "" {
			return semver{}, false
		}
		v.pre = strings.Split(pre, ".")
		for _, id := range v.pre {
			if id == "" {
				return semver{}, false
			}
		}
	}
	return v, true
}

func (v semver) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch)
	if v.pre != nil {
		s += "-" + strings.Join(v.pre, ".")
	}
	return s
}

// compare semver önceliği: <0, 0, >0.
func (v semver) compare(o semver) int {
	for _, d := range []int{v.major - o.major, v.minor - o.minor, v.patch - o.patch} {
		if d != 0 {
			return d
		}
	}
	switch {
	case v.pre == nil && o.pre == nil:
		return 0
	case v.pre == nil:
		return 1 // kararlı sürüm ön sürümden büyüktür
	case o.pre == nil:
		return -1
	}
	for i := 0; i < len(v.pre) && i < len(o.pre); i++ {
		a, b := v.pre[i], o.pre[i]
		an, aerr := strconv.Atoi(a)
		bn, berr := strconv.Atoi(b)
		switch {
		case aerr == nil && berr == nil:
			if an != bn {
				return an - bn
			}
		case aerr == nil:
			return -1 // sayısal tanımlayıcı küçüktür
		case berr == nil:
			return 1
		default:
			if c := strings.Compare(a, b); c != 0 {
				return c
			}
		}
	}
	return len(v.pre) - len(o.pre)
}
