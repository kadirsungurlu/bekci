package backup

// Uptime Kuma'dan içe aktarma: Ayarlar → Yedekle'den alınan JSON yedeği
// (Kuma 1.x) veya doğrudan kuma.db SQLite dosyası okunur.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/kadirsungurlu/bekci/internal/check"

	_ "modernc.org/sqlite"
)

// Okuma sınırları: bozuk veya kötü niyetli bir dosya belleği doldurmasın.
const (
	maxKumaMonitors      = 5000
	maxKumaNotifications = 1000
	maxKumaTags          = 5000
	maxKumaLinks         = 200000
)

// kumaData iki kaynaktan (JSON yedeği, SQLite) ortak ara biçim.
type kumaData struct {
	version       string
	monitors      []kv
	notifications []kumaNotification
	links         map[int64][]int64 // monitör → bildirim kimlikleri
	tags          map[int64]Tag     // etiket kimliği → ad/renk
	monitorTags   map[int64][]kumaMonitorTag
	proxies       map[int64]kv
	dockerHosts   map[int64]kv
}

type kumaNotification struct {
	id        int64
	name      string
	active    bool
	isDefault bool
	config    kv
}

type kumaMonitorTag struct {
	tagID int64
	value string
}

func newKumaData() *kumaData {
	return &kumaData{links: map[int64][]int64{}, tags: map[int64]Tag{}, monitorTags: map[int64][]kumaMonitorTag{},
		proxies: map[int64]kv{}, dockerHosts: map[int64]kv{}}
}

// IsSQLite verinin bir SQLite veritabanı dosyası olup olmadığını söyler.
func IsSQLite(head []byte) bool { return bytes.HasPrefix(head, []byte("SQLite format 3\x00")) }

// FromKumaJSON Uptime Kuma 1.x JSON yedeğini dönüştürür.
func FromKumaJSON(data []byte) (*Result, error) {
	var b struct {
		Version          string `json:"version"`
		NotificationList []kv   `json:"notificationList"`
		MonitorList      []kv   `json:"monitorList"`
		ProxyList        []kv   `json:"proxyList"`
	}
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("Uptime Kuma yedeği okunamadı: %v", err)
	}
	if b.MonitorList == nil && b.NotificationList == nil {
		return nil, errors.New("Bu dosya bir Uptime Kuma yedeği değil (monitorList yok)")
	}
	if len(b.MonitorList) > maxKumaMonitors || len(b.NotificationList) > maxKumaNotifications {
		return nil, errors.New("Yedekte çok fazla kayıt var")
	}
	d := newKumaData()
	d.version = b.Version
	for _, n := range b.NotificationList {
		d.notifications = append(d.notifications, kumaNotificationOf(n))
	}
	for _, p := range b.ProxyList {
		d.proxies[int64(p.int("id"))] = p
	}
	for _, m := range b.MonitorList {
		id := int64(m.int("id"))
		d.monitors = append(d.monitors, m)
		// notificationIDList: {"1": true, "3": true}
		if nl, ok := m["notificationIDList"].(map[string]any); ok {
			ids := make([]int64, 0, len(nl))
			for k, v := range nl {
				n, err := strconv.ParseInt(k, 10, 64)
				if err == nil && kv(map[string]any{"v": v}).bool("v") {
					ids = append(ids, n)
				}
			}
			sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
			d.links[id] = ids
		}
		if tl, ok := m["tags"].([]any); ok {
			for _, t := range tl {
				tm, ok := t.(map[string]any)
				if !ok {
					continue
				}
				tag := kv(tm)
				tid := int64(tag.int("tag_id", "tagId", "id"))
				if _, ok := d.tags[tid]; !ok {
					d.tags[tid] = Tag{Name: tag.str("name"), Color: tag.str("color")}
				}
				d.monitorTags[id] = append(d.monitorTags[id], kumaMonitorTag{tid, tag.str("value")})
			}
		}
	}
	return convertKuma(d), nil
}

func kumaNotificationOf(row kv) kumaNotification {
	cfg := kv{}
	switch c := row["config"].(type) {
	case string:
		json.Unmarshal([]byte(c), &cfg)
	case []byte:
		json.Unmarshal(c, &cfg)
	case map[string]any:
		cfg = c
	}
	name := row.str("name")
	if name == "" {
		name = cfg.str("name")
	}
	active := true
	if row.has("active") && row["active"] != nil {
		active = row.bool("active")
	}
	return kumaNotification{
		id: int64(row.int("id")), name: name, active: active,
		isDefault: row.bool("is_default", "isDefault") || cfg.bool("isDefault"), config: cfg,
	}
}

// FromKumaDB kuma.db SQLite dosyasını salt okunur açıp dönüştürür. path
// geçici bir kopya olmalıdır (çalışan bir Kuma'nın dosyası değil).
func FromKumaDB(ctx context.Context, path string) (*Result, error) {
	// immutable: dosya değişmez kabul edilir, kilit ve -wal/-shm dosyası gerekmez;
	// query_only: hiçbir yazma yapılamaz.
	db, err := sql.Open("sqlite", "file:"+path+"?immutable=1&_pragma=query_only(1)")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("Veritabanı açılamadı: %v", err)
	}
	d := newKumaData()
	d.monitors, err = kumaRows(ctx, db, "SELECT * FROM monitor ORDER BY id", maxKumaMonitors)
	if err != nil {
		if isNoTable(err) {
			return nil, errors.New("Bu dosya bir Uptime Kuma veritabanı değil (monitor tablosu yok)")
		}
		return nil, err
	}
	notifs, err := kumaRows(ctx, db, "SELECT * FROM notification ORDER BY id", maxKumaNotifications)
	if err != nil && !isNoTable(err) {
		return nil, err
	}
	for _, n := range notifs {
		d.notifications = append(d.notifications, kumaNotificationOf(n))
	}
	links, err := kumaRows(ctx, db, "SELECT monitor_id, notification_id FROM monitor_notification ORDER BY notification_id", maxKumaLinks)
	if err != nil && !isNoTable(err) {
		return nil, err
	}
	for _, l := range links {
		mid := int64(l.int("monitor_id"))
		d.links[mid] = append(d.links[mid], int64(l.int("notification_id")))
	}
	tags, err := kumaRows(ctx, db, "SELECT * FROM tag ORDER BY id", maxKumaTags)
	if err != nil && !isNoTable(err) {
		return nil, err
	}
	for _, t := range tags {
		d.tags[int64(t.int("id"))] = Tag{Name: t.str("name"), Color: t.str("color")}
	}
	mtags, err := kumaRows(ctx, db, "SELECT monitor_id, tag_id, value FROM monitor_tag ORDER BY id", maxKumaLinks)
	if err != nil && !isNoTable(err) {
		return nil, err
	}
	for _, t := range mtags {
		mid := int64(t.int("monitor_id"))
		d.monitorTags[mid] = append(d.monitorTags[mid], kumaMonitorTag{int64(t.int("tag_id")), t.str("value")})
	}
	proxies, err := kumaRows(ctx, db, "SELECT * FROM proxy", maxKumaTags)
	if err != nil && !isNoTable(err) {
		return nil, err
	}
	for _, p := range proxies {
		d.proxies[int64(p.int("id"))] = p
	}
	hosts, err := kumaRows(ctx, db, "SELECT * FROM docker_host", maxKumaTags)
	if err != nil && !isNoTable(err) {
		return nil, err
	}
	for _, h := range hosts {
		d.dockerHosts[int64(h.int("id"))] = h
	}
	return convertKuma(d), nil
}

func isNoTable(err error) bool { return err != nil && strings.Contains(err.Error(), "no such table") }

func kumaRows(ctx context.Context, db *sql.DB, q string, limit int) ([]kv, error) {
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out []kv
	for rows.Next() {
		if len(out) >= limit {
			return nil, errors.New("Veritabanında çok fazla kayıt var")
		}
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make(kv, len(cols))
		for i, c := range cols {
			row[c] = vals[i]
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// Dönüştürme ------------------------------------------------------------------------

func convertKuma(d *kumaData) *Result {
	res := newResult()
	if d.version != "" {
		res.Warnings = append(res.Warnings, "Uptime Kuma sürümü: "+d.version)
	}
	res.Warnings = append(res.Warnings,
		"Kontrol geçmişi, olaylar, durum sayfaları ve bakım pencereleri aktarılmaz; yalnızca monitörler, bildirim kanalları ve etiketler aktarılır.")

	// Bildirim kanalları
	usedNames := map[string]bool{}
	notifNames := map[int64]string{}
	for _, n := range d.notifications {
		typ := n.config.str("type")
		name := n.name
		if name == "" {
			name = typ
		}
		mapped, cfg, notes, reason := convertKumaNotification(typ, n.config)
		if reason != "" {
			res.skip("notification", name, typ, reason)
			continue
		}
		unique := UniqueName(name, usedNames)
		if unique != strings.TrimSpace(name) {
			notes = append(notes, fmt.Sprintf("Aynı adda başka bir kanal olduğu için “%s” olarak adlandırıldı", unique))
		}
		b, _ := json.Marshal(cfg)
		res.Doc.Notifications = append(res.Doc.Notifications, Notification{
			Name: unique, Type: mapped, Config: b, IsDefault: n.isDefault, Active: n.active, Notes: notes,
		})
		notifNames[n.id] = unique
	}

	// Etiketler (ad büyük/küçük harf duyarsız tekil)
	tagIDs := make([]int64, 0, len(d.tags))
	for id := range d.tags {
		tagIDs = append(tagIDs, id)
	}
	sort.Slice(tagIDs, func(i, j int) bool { return tagIDs[i] < tagIDs[j] })
	tagNames := map[int64]string{}
	seenTag := map[string]string{}
	for _, id := range tagIDs {
		t := d.tags[id]
		name := strings.TrimSpace(t.Name)
		if name == "" {
			continue
		}
		if r := []rune(name); len(r) > 50 {
			name = string(r[:50])
		}
		key := strings.ToLower(name)
		if existing, ok := seenTag[key]; ok {
			tagNames[id] = existing
			continue
		}
		seenTag[key] = name
		tagNames[id] = name
		res.Doc.Tags = append(res.Doc.Tags, Tag{Name: name, Color: NormalizeColor(t.Color)})
	}

	// Monitörler
	_, hasGroup := check.Get("group")
	children := map[int64][]int64{}
	for _, m := range d.monitors {
		if p := int64(m.int("parent")); p > 0 {
			children[p] = append(children[p], int64(m.int("id")))
		}
	}
	converted := map[int64]bool{}
	var groups []int // res.Doc.Monitors içindeki grup monitörlerinin sırası
	for _, m := range d.monitors {
		id := int64(m.int("id"))
		name := strings.TrimSpace(m.str("name"))
		typ := m.str("type")
		if typ == "group" && !hasGroup {
			res.skip("monitor", name, typ, "Grup monitörü tipi bu sürümde yok; alt monitörler grupsuz aktarıldı")
			continue
		}
		out, reason := convertKumaMonitor(d, m)
		if reason != "" {
			res.skip("monitor", name, typ, reason)
			continue
		}
		for _, nid := range d.links[id] {
			if n, ok := notifNames[nid]; ok {
				out.Notifications = append(out.Notifications, n)
			}
		}
		seenMT := map[MonitorTag]bool{}
		for _, t := range d.monitorTags[id] {
			n, ok := tagNames[t.tagID]
			if !ok {
				continue
			}
			mt := MonitorTag{Name: n, Value: strings.TrimSpace(t.value)}
			if r := []rune(mt.Value); len(r) > 100 {
				mt.Value = string(r[:100])
			}
			if !seenMT[mt] {
				seenMT[mt] = true
				out.Tags = append(out.Tags, mt)
			}
		}
		converted[id] = true
		if out.Type == "group" {
			groups = append(groups, len(res.Doc.Monitors))
		}
		res.Doc.Monitors = append(res.Doc.Monitors, out)
	}
	// Grup monitörleri alt monitörlerine Kuma kimlikleriyle başvurur; içe
	// aktarmada yeni kimliklere çevrilir.
	for _, i := range groups {
		g := &res.Doc.Monitors[i]
		ids := []int64{}
		for _, c := range children[g.ID] {
			if converted[c] {
				ids = append(ids, c)
			}
		}
		g.Config, _ = json.Marshal(map[string]any{"monitor_ids": ids})
	}
	return res
}

// convertKumaMonitor tek bir Kuma monitörünü çevirir; çevrilemiyorsa neden döner.
func convertKumaMonitor(d *kumaData, m kv) (Monitor, string) {
	typ := m.str("type")
	out := Monitor{
		ID:            int64(m.int("id")),
		Name:          strings.TrimSpace(m.str("name")),
		Description:   strings.TrimSpace(m.str("description")),
		Active:        m.bool("active"),
		UpsideDown:    m.bool("upside_down", "upsideDown"),
		Notifications: []string{},
		Tags:          []MonitorTag{},
	}
	if !m.has("active") {
		out.Active = true
	}
	if r := []rune(out.Name); len(r) > 100 {
		out.Name = string(r[:100])
		out.Notes = append(out.Notes, "Ad 100 karaktere kısaltıldı")
	}
	if r := []rune(out.Description); len(r) > 500 {
		out.Description = string(r[:500])
		out.Notes = append(out.Notes, "Açıklama 500 karaktere kısaltıldı")
	}
	kumaTimings(m, &out)

	var cfg map[string]any
	switch typ {
	case "http", "keyword", "json-query":
		out.Type = "http"
		cfg = kumaHTTPConfig(d, m, typ, &out)
	case "real-browser":
		out.Type = "http"
		cfg = kumaHTTPConfig(d, m, typ, &out)
		out.Notes = append(out.Notes, "Gerçek tarayıcı kontrolü desteklenmiyor; HTTP monitörü olarak aktarıldı")
	case "port":
		out.Type = "tcp"
		cfg = map[string]any{"host": strings.TrimSpace(m.str("hostname")), "port": m.int("port")}
	case "ping":
		out.Type = "ping"
		cfg = map[string]any{"host": strings.TrimSpace(m.str("hostname"))}
	case "dns":
		out.Type = "dns"
		cfg = map[string]any{
			"host": strings.TrimSpace(m.str("hostname")), "server": strings.TrimSpace(m.str("dns_resolve_server")),
			"port": m.int("port"), "record_type": m.str("dns_resolve_type"),
		}
		if p := m.int("port"); p <= 0 || p > 65535 {
			cfg["port"] = 53
		}
	case "push":
		out.Type = check.TypePush
		cfg = map[string]any{}
		out.PushToken = m.str("push_token", "pushToken")
	case "group":
		out.Type = "group"
		cfg = map[string]any{"monitor_ids": []int64{}} // alt monitörler sonradan doldurulur
	default:
		t, c, reason := kumaExtraType(d, m, typ)
		if reason != "" {
			return out, reason
		}
		out.Type, cfg = t, c
	}
	out.Config, _ = json.Marshal(cfg)
	return out, ""
}

// kumaTimings aralıkları bu uygulamanın sınırlarına uyarlar.
func kumaTimings(m kv, out *Monitor) {
	interval := m.int("interval")
	if interval <= 0 {
		interval = 60
	}
	out.Interval = clamp(interval, 20, 86400)
	if out.Interval != interval {
		out.Notes = append(out.Notes, fmt.Sprintf("Kontrol aralığı %d sn → %d sn", interval, out.Interval))
	}
	retry := m.int("retry_interval", "retryInterval")
	if retry <= 0 {
		retry = out.Interval
	}
	out.RetryInterval = clamp(retry, 20, 86400)
	retries := m.int("maxretries", "max_retries", "maxRetries")
	out.MaxRetries = clamp(retries, 0, 20)
	if out.MaxRetries != retries {
		out.Notes = append(out.Notes, fmt.Sprintf("Tekrar deneme sayısı %d → %d", retries, out.MaxRetries))
	}
	// Kuma'da zaman aşımı saniye (ondalıklı olabilir); 0 veya yoksa aralığın %80'i.
	timeout := m.int("timeout")
	if timeout <= 0 {
		timeout = out.Interval * 8 / 10
	}
	out.Timeout = clamp(timeout, 1, 300)
	// Kuma'da da "kaç kontrolde bir hatırlatma" anlamında.
	out.ResendEvery = clamp(m.int("resend_interval", "resendInterval"), 0, 10000)
}

func kumaHTTPConfig(d *kumaData, m kv, typ string, out *Monitor) map[string]any {
	cfg := map[string]any{
		"url":         strings.TrimSpace(m.str("url")),
		"method":      strings.ToUpper(strings.TrimSpace(m.str("method"))),
		"body":        m.str("body"),
		"ignore_tls":  m.bool("ignore_tls", "ignoreTls"),
		"cert_expiry": m.bool("expiry_notification", "expiryNotification"),
	}
	if !m.has("expiry_notification") && !m.has("expiryNotification") {
		cfg["cert_expiry"] = true
	}
	if m.has("maxredirects") || m.has("max_redirects") {
		cfg["max_redirects"] = clamp(m.int("maxredirects", "max_redirects"), 0, 30)
	}
	if codes := kumaStatusCodes(m); len(codes) > 0 {
		cfg["accepted_codes"] = codes
	}
	headers, note := kumaHeaders(m.str("headers"))
	if note != "" {
		out.Notes = append(out.Notes, note)
	}
	if body := strings.TrimSpace(m.str("body")); body != "" {
		ct := map[string]string{"form": "application/x-www-form-urlencoded", "xml": "application/xml"}[m.str("http_body_encoding", "httpBodyEncoding")]
		if ct != "" && !strings.Contains(strings.ToLower(headers), "content-type:") {
			headers = strings.TrimSpace(headers + "\nContent-Type: " + ct)
		}
	}
	cfg["headers"] = headers

	switch strings.ToLower(m.str("auth_method", "authMethod")) {
	case "ntlm":
		out.Notes = append(out.Notes, "NTLM kimlik doğrulaması desteklenmiyor; kimlik bilgileri aktarılmadı")
	case "mtls":
		cfg["tls_cert"], cfg["tls_key"] = m.str("tls_cert", "tlsCert"), m.str("tls_key", "tlsKey")
	case "oauth2-cc":
		cfg["oauth_token_url"] = m.str("oauth_token_url", "oauthTokenUrl")
		cfg["oauth_client_id"] = m.str("oauth_client_id", "oauthClientId")
		cfg["oauth_client_secret"] = m.str("oauth_client_secret", "oauthClientSecret")
		cfg["oauth_scopes"] = m.str("oauth_scopes", "oauthScopes")
		if strings.Contains(m.str("oauth_auth_method", "oauthAuthMethod"), "post") {
			cfg["oauth_auth_style"] = "body"
		}
	default:
		if u := m.str("basic_auth_user", "basicAuthUser"); u != "" || m.str("basic_auth_pass", "basicAuthPass") != "" {
			cfg["basic_user"], cfg["basic_pass"] = u, m.str("basic_auth_pass", "basicAuthPass")
		}
	}
	if ca := strings.TrimSpace(m.str("tls_ca", "tlsCa")); ca != "" {
		cfg["tls_ca"] = ca
	}
	if pid := int64(m.int("proxy_id", "proxyId")); pid > 0 {
		if p, ok := d.proxies[pid]; ok {
			kumaProxy(p, cfg, out)
		} else {
			out.Notes = append(out.Notes, "Proxy ayarı yedekte bulunamadı; proxy olmadan aktarıldı")
		}
	}

	switch typ {
	case "keyword":
		cfg["keyword"] = m.str("keyword")
		cfg["keyword_invert"] = m.bool("invert_keyword", "invertKeyword")
		cfg["keyword_case"] = true // Kuma büyük/küçük harf duyarlı arar
	case "json-query":
		path, note := kumaJSONPath(m.str("json_path", "jsonPath"))
		if note != "" {
			out.Notes = append(out.Notes, note)
		}
		cfg["json_path"] = path
		cfg["json_expected"] = m.str("expected_value", "expectedValue")
		op := m.str("json_path_operator", "jsonPathOperator")
		if op == "" || op == "=" {
			op = "=="
		}
		if !map[string]bool{"==": true, "!=": true, "contains": true, ">": true, ">=": true, "<": true, "<=": true}[op] {
			out.Notes = append(out.Notes, fmt.Sprintf("JSON karşılaştırması “%s” desteklenmiyor; “==” kullanıldı", op))
			op = "=="
		}
		cfg["json_op"] = op
	}
	return cfg
}

func kumaStatusCodes(m kv) []string {
	var list []any
	switch v := m["accepted_statuscodes"].(type) {
	case []any:
		list = v
	}
	if list == nil {
		raw := m.str("accepted_statuscodes_json", "accepted_statuscodes")
		json.Unmarshal([]byte(raw), &list)
	}
	out := []string{}
	for _, c := range list {
		if s := strings.TrimSpace(kv{"v": c}.str("v")); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// kumaHeaders Kuma'nın JSON başlık nesnesini "Ad: değer" satırlarına çevirir.
func kumaHeaders(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return "", "Başlıklar JSON olarak okunamadı; aktarılmadı"
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var lines []string
	for _, k := range keys {
		name := strings.TrimSpace(k)
		if name == "" || strings.ContainsAny(name, " \t:\r\n") {
			continue
		}
		v := strings.NewReplacer("\r", " ", "\n", " ").Replace(kv{"v": obj[k]}.str("v"))
		lines = append(lines, name+": "+v)
	}
	return strings.Join(lines, "\n"), ""
}

var jsonataSimple = regexp.MustCompile(`^[A-Za-z0-9_\-]+(\.[A-Za-z0-9_\-]+|\[\d+\])*$`)
var jsonataIndex = regexp.MustCompile(`\[(\d+)\]`)

// kumaJSONPath Kuma'nın JSONata ifadesini gjson yoluna çevirir (yalnızca
// basit "a.b[0].c" biçimleri birebir karşılanır).
func kumaJSONPath(p string) (string, string) {
	p = strings.TrimSpace(p)
	switch {
	case p == "$":
		p = ""
	case strings.HasPrefix(p, "$."):
		p = p[2:]
	}
	if p == "" {
		return "", ""
	}
	if !jsonataSimple.MatchString(p) {
		return p, "JSON sorgusu (JSONata) olduğu gibi aktarıldı; bu uygulama gjson yolu kullanır, kontrol edin"
	}
	return jsonataIndex.ReplaceAllString(p, ".$1"), ""
}

func kumaProxy(p kv, cfg map[string]any, out *Monitor) {
	scheme := strings.ToLower(p.str("protocol"))
	switch scheme {
	case "socks":
		scheme = "socks5"
	case "http", "https", "socks5", "socks5h":
	default:
		out.Notes = append(out.Notes, fmt.Sprintf("Proxy protokolü “%s” desteklenmiyor; proxy olmadan aktarıldı", scheme))
		return
	}
	host := strings.TrimSpace(p.str("host"))
	port := p.int("port")
	if host == "" || port <= 0 {
		return
	}
	u := url.URL{Scheme: scheme, Host: host + ":" + strconv.Itoa(port)}
	cfg["proxy_url"] = u.String()
	if p.bool("auth") {
		cfg["proxy_user"], cfg["proxy_pass"] = p.str("username"), p.str("password")
	}
}

// kumaExtraType Kuma'nın diğer tiplerini, bu uygulamada aynı adla kayıtlı bir
// monitör tipi varsa çevirir. Alan adları o tiplerin ayarlarıyla uyumlu
// olmalıdır; uyumsuzsa içe aktarmadaki doğrulama kaydı atlar ve nedenini söyler.
func kumaExtraType(d *kumaData, m kv, typ string) (string, map[string]any, string) {
	target := map[string]string{
		"docker": "docker", "mysql": "mysql", "postgres": "postgres", "sqlserver": "mssql",
		"mongodb": "mongodb", "redis": "redis", "grpc-keyword": "grpc", "mqtt": "mqtt",
	}[typ]
	if target == "" {
		return "", nil, "Bu monitör tipi desteklenmiyor"
	}
	if _, ok := check.Get(target); !ok {
		return "", nil, "Bu monitör tipi bu sürümde yok"
	}
	cfg := map[string]any{}
	conn := strings.TrimSpace(m.str("database_connection_string", "databaseConnectionString"))
	query := strings.TrimSpace(m.str("database_query", "databaseQuery"))
	switch typ {
	case "docker":
		cfg["container"] = m.str("docker_container", "dockerContainer")
		if h, ok := d.dockerHosts[int64(m.int("docker_host", "dockerHost"))]; ok {
			if ep := kumaDockerEndpoint(h.str("docker_daemon", "dockerDaemon")); ep != "" {
				cfg["endpoint"] = ep
			}
		}
	case "mysql", "postgres", "redis":
		// Kuma bağlantıyı tek bir URL olarak tutar: şema://kullanıcı:şifre@sunucu:port/veritabanı
		u, err := url.Parse(conn)
		if err != nil || u.Hostname() == "" {
			return "", nil, "Veritabanı bağlantı adresi okunamadı"
		}
		cfg["host"] = u.Hostname()
		if p, err := strconv.Atoi(u.Port()); err == nil {
			cfg["port"] = p
		}
		if u.User != nil {
			cfg["username"] = u.User.Username()
			if pw, ok := u.User.Password(); ok {
				cfg["password"] = pw
			}
		}
		db := strings.TrimPrefix(u.Path, "/")
		switch typ {
		case "redis":
			if n, err := strconv.Atoi(db); err == nil {
				cfg["db"] = n
			}
			if u.Scheme == "rediss" {
				cfg["tls"] = true
			}
		default:
			if db != "" {
				cfg["database"] = db
			}
			if query != "" {
				cfg["query"] = query
			}
			if typ == "postgres" {
				if sm := u.Query().Get("sslmode"); sm == "disable" || sm == "require" || sm == "verify-full" {
					cfg["sslmode"] = sm
				}
			}
		}
	case "sqlserver":
		// ADO biçimi: Server=sunucu,1433;Database=vt;User Id=k;Password=ş;Encrypt=true
		kv := kumaADO(conn)
		server := strings.TrimPrefix(firstNonEmpty(kv["server"], kv["data source"], kv["address"], kv["addr"]), "tcp:")
		host, port, _ := strings.Cut(server, ",")
		if strings.TrimSpace(host) == "" {
			return "", nil, "SQL Server bağlantı metni okunamadı"
		}
		cfg["host"] = strings.TrimSpace(host)
		if p, err := strconv.Atoi(strings.TrimSpace(port)); err == nil {
			cfg["port"] = p
		}
		if v := firstNonEmpty(kv["database"], kv["initial catalog"]); v != "" {
			cfg["database"] = v
		}
		if v := firstNonEmpty(kv["user id"], kv["uid"], kv["user"]); v != "" {
			cfg["username"] = v
		}
		if v := firstNonEmpty(kv["password"], kv["pwd"]); v != "" {
			cfg["password"] = v
		}
		if e := strings.ToLower(kv["encrypt"]); e == "true" || e == "yes" {
			cfg["encrypt"] = true
		}
		if query != "" {
			cfg["query"] = query
		}
	case "mongodb":
		cfg["uri"] = conn
	case "grpc-keyword":
		t := m.str("grpc_url", "grpcUrl")
		if i := strings.Index(t, "://"); i >= 0 {
			t = t[i+3:]
		}
		cfg["target"] = strings.TrimSuffix(t, "/")
		if svc := m.str("grpc_service_name", "grpcServiceName"); svc != "" {
			cfg["service"] = svc
		}
		cfg["tls"] = m.bool("grpc_enable_tls", "grpcEnableTls")
	case "mqtt":
		host := strings.TrimSpace(m.str("hostname"))
		scheme := "tcp"
		if i := strings.Index(host, "://"); i >= 0 {
			scheme = map[string]string{"mqtt": "tcp", "mqtts": "ssl", "ws": "ws", "wss": "wss"}[strings.ToLower(host[:i])]
			if scheme == "" {
				scheme = "tcp"
			}
			host = host[i+3:]
		}
		port := m.int("port")
		if port == 0 {
			port = 1883
		}
		cfg["broker_url"] = fmt.Sprintf("%s://%s:%d", scheme, host, port)
		if v := m.str("mqtt_topic", "mqttTopic"); v != "" {
			cfg["topic"] = v
			if k := m.str("mqtt_success_message", "mqttSuccessMessage"); k != "" {
				cfg["keyword"] = k
			}
		}
		if v := m.str("mqtt_username", "mqttUsername"); v != "" {
			cfg["username"] = v
		}
		if v := m.str("mqtt_password", "mqttPassword"); v != "" {
			cfg["password"] = v
		}
	}
	return target, cfg, ""
}

// kumaDockerEndpoint Kuma'nın Docker adresini ("/var/run/docker.sock",
// "tcp://sunucu:2375") bizim biçimimize çevirir.
func kumaDockerEndpoint(daemon string) string {
	d := strings.TrimSpace(daemon)
	if strings.HasPrefix(d, "/") {
		return "unix://" + d
	}
	return d
}

// kumaADO "Anahtar=Değer;…" biçimindeki SQL Server bağlantı metnini çözer
// (anahtarlar küçük harfe çevrilir).
func kumaADO(s string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(s, ";") {
		k, v, ok := strings.Cut(part, "=")
		if ok {
			out[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
		}
	}
	return out
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
