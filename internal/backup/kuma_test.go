package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kadirsa1105/uptime-kadir-app/internal/check"
	"github.com/kadirsa1105/uptime-kadir-app/internal/notify"
)

// Grup tipi bu dalda henüz yoksa dönüştürme testleri için sahtesi kaydedilir.
type fakeGroup struct{}

func (fakeGroup) Normalize(c json.RawMessage) (json.RawMessage, error) { return c, nil }
func (fakeGroup) Check(context.Context, json.RawMessage) check.Result  { return check.Result{Up: true} }
func (fakeGroup) Target(json.RawMessage) string                        { return "Grup" }

func init() {
	if _, ok := check.Get("group"); !ok {
		check.Register("group", fakeGroup{})
	}
}

// kumaSchema Uptime Kuma 1.23 şemasının içe aktarmada okunan kısmı (sütun
// adları Kuma'daki gibi).
const kumaSchema = `
CREATE TABLE monitor (
	id INTEGER PRIMARY KEY AUTOINCREMENT, name VARCHAR(150), active BOOLEAN DEFAULT 1, user_id INTEGER,
	interval INTEGER DEFAULT 20, url TEXT, type VARCHAR(20), weight INTEGER DEFAULT 2000, hostname VARCHAR(255),
	port INTEGER, created_date DATETIME, keyword VARCHAR(255), maxretries INTEGER DEFAULT 0,
	ignore_tls BOOLEAN DEFAULT 0, upside_down BOOLEAN DEFAULT 0, maxredirects INTEGER DEFAULT 10,
	accepted_statuscodes_json TEXT DEFAULT '["200-299"]', dns_resolve_type VARCHAR(5), dns_resolve_server VARCHAR(255),
	dns_last_result VARCHAR(255), retry_interval INTEGER DEFAULT 0, push_token VARCHAR(20), method TEXT DEFAULT 'GET',
	body TEXT, headers TEXT, basic_auth_user TEXT, basic_auth_pass TEXT, docker_host INTEGER, docker_container VARCHAR(255),
	proxy_id INTEGER, expiry_notification BOOLEAN DEFAULT 1, mqtt_topic TEXT, mqtt_success_message VARCHAR(255),
	mqtt_username VARCHAR(255), mqtt_password VARCHAR(255), database_connection_string VARCHAR(2000),
	database_query TEXT, auth_method VARCHAR(250), auth_domain TEXT, auth_workstation TEXT, grpc_url VARCHAR(255),
	grpc_protobuf TEXT, grpc_body TEXT, grpc_metadata TEXT, grpc_method VARCHAR(255), grpc_service_name VARCHAR(255),
	grpc_enable_tls BOOLEAN DEFAULT 0, packet_size INTEGER DEFAULT 56, resend_interval INTEGER DEFAULT 0,
	description TEXT, parent INTEGER, invert_keyword BOOLEAN DEFAULT 0, json_path TEXT, expected_value VARCHAR(255),
	http_body_encoding VARCHAR(25), tls_ca TEXT, tls_cert TEXT, tls_key TEXT, oauth_client_id TEXT,
	oauth_client_secret TEXT, oauth_token_url TEXT, oauth_scopes TEXT, oauth_auth_method TEXT DEFAULT 'client_secret_basic',
	timeout DOUBLE DEFAULT 0
);
CREATE TABLE notification (id INTEGER PRIMARY KEY AUTOINCREMENT, name VARCHAR(255), config VARCHAR(255),
	active BOOLEAN DEFAULT 1, user_id INTEGER, is_default BOOLEAN DEFAULT 0);
CREATE TABLE monitor_notification (id INTEGER PRIMARY KEY AUTOINCREMENT, monitor_id INTEGER, notification_id INTEGER);
CREATE TABLE tag (id INTEGER PRIMARY KEY AUTOINCREMENT, name VARCHAR(255), color VARCHAR(255), created_date DATETIME);
CREATE TABLE monitor_tag (id INTEGER PRIMARY KEY AUTOINCREMENT, monitor_id INTEGER, tag_id INTEGER, value TEXT);
CREATE TABLE proxy (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INT, protocol VARCHAR(10), host VARCHAR(255),
	port SMALLINT, auth BOOLEAN, username VARCHAR(255), password VARCHAR(255), active BOOLEAN DEFAULT 1,
	"default" BOOLEAN DEFAULT 0, created_date DATETIME);
CREATE TABLE docker_host (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INT, docker_daemon VARCHAR(255),
	docker_type VARCHAR(255), name VARCHAR(255));
`

const kumaFixture = `
INSERT INTO proxy (id, protocol, host, port, auth, username, password) VALUES (1, 'socks', '10.0.0.9', 1080, 1, 'pxu', 'pxp');
INSERT INTO monitor (id, name, type, url, method, body, headers, basic_auth_user, basic_auth_pass, auth_method,
	accepted_statuscodes_json, maxredirects, ignore_tls, interval, retry_interval, maxretries, timeout, resend_interval,
	upside_down, active, description, http_body_encoding, proxy_id, expiry_notification)
VALUES (1, 'Ana site', 'http', 'https://kadir.app', 'POST', 'a=1', '{"X-Token":"abc","Accept":"text/html"}', 'admin', 'gizli',
	'basic', '["200-299","301"]', 3, 1, 30, 25, 3, 12.5, 5, 0, 1, 'açıklama', 'form', 1, 0);
INSERT INTO monitor (id, name, type, url, keyword, invert_keyword, interval, active, parent)
	VALUES (2, 'Kelime', 'keyword', 'https://kadir.app/durum', 'Hata', 1, 10, 0, 7);
INSERT INTO monitor (id, name, type, url, json_path, expected_value, interval, parent)
	VALUES (3, 'JSON', 'json-query', 'https://api.kadir.app/saglik', '$.data.items[0].status', 'ok', 60, 7);
INSERT INTO monitor (id, name, type, hostname, port, interval) VALUES (4, 'SSH', 'port', 'sunucu.kadir.app', 22, 60);
INSERT INTO monitor (id, name, type, hostname, interval) VALUES (5, 'Ping', 'ping', '1.1.1.1', 60);
INSERT INTO monitor (id, name, type, hostname, dns_resolve_server, dns_resolve_type, port, interval)
	VALUES (6, 'DNS', 'dns', 'kadir.app', '8.8.8.8', 'MX', 53, 60);
INSERT INTO monitor (id, name, type, interval) VALUES (7, 'Grup', 'group', 60);
INSERT INTO monitor (id, name, type, push_token, interval, maxretries) VALUES (8, 'Yedek işi', 'push', 'Kuma1234Token', 3600, 50);
INSERT INTO monitor (id, name, type, docker_container, docker_host, interval) VALUES (9, 'Konteyner', 'docker', 'db', 1, 60);
INSERT INTO monitor (id, name, type, hostname, interval) VALUES (10, 'Oyun', 'steam', 'oyun.kadir.app', 60);
INSERT INTO monitor (id, name, type, url, auth_method, tls_cert, tls_key, tls_ca, interval)
	VALUES (11, 'mTLS', 'http', 'https://ic.kadir.app', 'mtls', 'CERT', 'KEY', 'CA', 60);
INSERT INTO monitor (id, name, type, url, auth_method, oauth_token_url, oauth_client_id, oauth_client_secret,
	oauth_scopes, oauth_auth_method, interval)
	VALUES (12, 'OAuth', 'http', 'https://api.kadir.app', 'oauth2-cc', 'https://auth.kadir.app/token', 'cid', 'csecret',
	'okuma', 'client_secret_post', 60);

INSERT INTO notification (id, name, config, active, is_default) VALUES
	(1, 'Telegram', '{"name":"Telegram","type":"telegram","telegramBotToken":"123:ABC","telegramChatID":"-100","isDefault":true}', 1, 1),
	(2, 'E-posta', '{"type":"smtp","smtpHost":"smtp.kadir.app","smtpPort":465,"smtpSecure":true,"smtpUsername":"u","smtpPassword":"p","smtpFrom":"uptime@kadir.app","smtpTo":"a@kadir.app","smtpCC":"b@kadir.app"}', 1, 0),
	(3, 'Telegram', '{"type":"telegram","telegramBotToken":"456:DEF","telegramChatID":"42"}', 0, 0),
	(4, 'Apprise', '{"type":"apprise","appriseURL":"tgram://x"}', 1, 0),
	(5, 'Feishu', '{"type":"Feishu","feishuWebHookUrl":"https://x"}', 1, 0),
	(6, 'Kanca', '{"type":"webhook","webhookURL":"https://hook.kadir.app","webhookContentType":"custom","webhookAdditionalHeaders":"{\"Authorization\":\"Bearer t\"}"}', 1, 0),
	(7, 'Roket', '{"type":"rocket.chat","rocketwebhookURL":"https://chat.kadir.app/hooks/x","rocketchannel":"#izleme","rocketusername":"Uptime"}', 1, 0);
INSERT INTO monitor_notification (monitor_id, notification_id) VALUES (1, 1), (1, 2), (1, 4), (4, 3), (8, 6);

INSERT INTO tag (id, name, color) VALUES (1, 'ortam', '#DC2626'), (2, 'Takım', 'blue'), (3, 'ORTAM', '#000');
INSERT INTO monitor_tag (monitor_id, tag_id, value) VALUES (1, 1, 'canlı'), (1, 2, ''), (4, 3, 'test'), (1, 1, 'canlı');
`

func createKumaDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kuma.db")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{kumaSchema, kumaFixture} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func monitorByName(t *testing.T, d *Doc, name string) Monitor {
	t.Helper()
	for _, m := range d.Monitors {
		if m.Name == name {
			return m
		}
	}
	t.Fatalf("monitör yok: %s", name)
	return Monitor{}
}

func cfgOf(m Monitor) map[string]any {
	var c map[string]any
	json.Unmarshal(m.Config, &c)
	return c
}

func TestKumaDB(t *testing.T) {
	res, err := FromKumaDB(context.Background(), createKumaDB(t))
	if err != nil {
		t.Fatal(err)
	}
	d := res.Doc
	checkKumaDoc(t, res)

	// Veritabanına özgü: proxy ve etiket birleştirme.
	site := monitorByName(t, d, "Ana site")
	c := cfgOf(site)
	if c["proxy_url"] != "socks5://10.0.0.9:1080" || c["proxy_user"] != "pxu" || c["proxy_pass"] != "pxp" {
		t.Errorf("proxy: %v", c)
	}
	if len(d.Tags) != 2 || d.Tags[0].Name != "ortam" || d.Tags[0].Color != "#dc2626" || d.Tags[1].Color != DefaultTagColor {
		t.Errorf("etiketler: %+v", d.Tags)
	}
	if len(site.Tags) != 2 || site.Tags[0] != (MonitorTag{"ortam", "canlı"}) || site.Tags[1] != (MonitorTag{"Takım", ""}) {
		t.Errorf("monitör etiketleri: %+v", site.Tags)
	}
	// "ORTAM" etiketi "ortam" ile birleşir.
	if ssh := monitorByName(t, d, "SSH"); len(ssh.Tags) != 1 || ssh.Tags[0] != (MonitorTag{"ortam", "test"}) {
		t.Errorf("birleşen etiket: %+v", ssh.Tags)
	}
	// Aynı adlı ikinci Telegram kanalı ayrıştırılır ve bağlantı korunur.
	if ssh := monitorByName(t, d, "SSH"); len(ssh.Notifications) != 1 || ssh.Notifications[0] != "Telegram (2)" {
		t.Errorf("SSH bildirimleri: %v", ssh.Notifications)
	}
}

// checkKumaDoc JSON ve veritabanı kaynaklarında ortak beklentiler.
func checkKumaDoc(t *testing.T, res *Result) {
	t.Helper()
	d := res.Doc
	skipped := map[string]string{}
	for _, s := range res.Skipped {
		skipped[s.Name] = s.Reason
	}
	for _, name := range []string{"Konteyner", "Oyun", "Apprise", "Feishu"} {
		if _, ok := skipped[name]; !ok {
			t.Errorf("%s atlanmalıydı (atlananlar: %v)", name, skipped)
		}
	}

	site := monitorByName(t, d, "Ana site")
	c := cfgOf(site)
	if site.Type != "http" || c["url"] != "https://kadir.app" || c["method"] != "POST" || c["body"] != "a=1" ||
		c["basic_user"] != "admin" || c["basic_pass"] != "gizli" || c["ignore_tls"] != true || c["cert_expiry"] != false ||
		c["max_redirects"] != float64(3) {
		t.Errorf("http ayarı: %v", c)
	}
	if h := c["headers"]; h != "Accept: text/html\nX-Token: abc\nContent-Type: application/x-www-form-urlencoded" {
		t.Errorf("başlıklar: %q", h)
	}
	if codes, _ := json.Marshal(c["accepted_codes"]); string(codes) != `["200-299","301"]` {
		t.Errorf("durum kodları: %s", codes)
	}
	if site.Interval != 30 || site.RetryInterval != 25 || site.MaxRetries != 3 || site.Timeout != 13 || site.ResendEvery != 5 ||
		!site.Active || site.Description != "açıklama" {
		t.Errorf("zamanlamalar: %+v", site)
	}
	if len(site.Notifications) != 2 || site.Notifications[0] != "Telegram" || site.Notifications[1] != "E-posta" {
		t.Errorf("bildirim bağlantıları: %v", site.Notifications)
	}

	kw := monitorByName(t, d, "Kelime")
	c = cfgOf(kw)
	if c["keyword"] != "Hata" || c["keyword_invert"] != true || c["keyword_case"] != true || kw.Active || kw.Interval != 20 {
		t.Errorf("keyword: %+v %v", kw, c)
	}
	c = cfgOf(monitorByName(t, d, "JSON"))
	if c["json_path"] != "data.items.0.status" || c["json_expected"] != "ok" || c["json_op"] != "==" {
		t.Errorf("json sorgusu: %v", c)
	}
	if c := cfgOf(monitorByName(t, d, "SSH")); c["host"] != "sunucu.kadir.app" || c["port"] != float64(22) {
		t.Errorf("port: %v", c)
	}
	if c := cfgOf(monitorByName(t, d, "DNS")); c["server"] != "8.8.8.8" || c["record_type"] != "MX" || c["port"] != float64(53) {
		t.Errorf("dns: %v", c)
	}
	push := monitorByName(t, d, "Yedek işi")
	if push.Type != "push" || push.PushToken != "Kuma1234Token" || push.MaxRetries != 20 || len(push.Notifications) != 1 {
		t.Errorf("push: %+v", push)
	}
	grp := monitorByName(t, d, "Grup")
	if c := cfgOf(grp); grp.Type != "group" || len(c["monitor_ids"].([]any)) != 2 {
		t.Errorf("grup: %v", c)
	}
	if c := cfgOf(monitorByName(t, d, "mTLS")); c["tls_cert"] != "CERT" || c["tls_key"] != "KEY" || c["tls_ca"] != "CA" {
		t.Errorf("mtls: %v", c)
	}
	if c := cfgOf(monitorByName(t, d, "OAuth")); c["oauth_client_secret"] != "csecret" || c["oauth_auth_style"] != "body" || c["oauth_scopes"] != "okuma" {
		t.Errorf("oauth: %v", c)
	}

	// Çevrilen her monitör ayarı hedef tipin doğrulamasından geçmeli (sahte
	// sertifikalı mTLS dışında).
	for _, m := range d.Monitors {
		if m.Name == "mTLS" {
			continue
		}
		ch, _ := check.Get(m.Type)
		if _, err := ch.Normalize(m.Config); err != nil {
			t.Errorf("%s ayarı geçersiz: %v (%s)", m.Name, err, m.Config)
		}
	}

	notifs := map[string]Notification{}
	for _, n := range d.Notifications {
		notifs[n.Name] = n
	}
	tg := notifs["Telegram"]
	var tc map[string]any
	json.Unmarshal(tg.Config, &tc)
	if tg.Type != "telegram" || tc["bot_token"] != "123:ABC" || tc["chat_id"] != "-100" || !tg.IsDefault || !tg.Active {
		t.Errorf("telegram: %+v %v", tg, tc)
	}
	if n := notifs["Telegram (2)"]; n.Active || n.IsDefault {
		t.Errorf("ikinci telegram: %+v", n)
	}
	em := notifs["E-posta"]
	json.Unmarshal(em.Config, &tc)
	if em.Type != "email" || tc["security"] != "tls" || tc["to"] != "a@kadir.app, b@kadir.app" || tc["password"] != "p" || len(em.Notes) == 0 {
		t.Errorf("e-posta: %+v %v", em, tc)
	}
	hook := notifs["Kanca"]
	json.Unmarshal(hook.Config, &tc)
	if tc["headers"] != "Authorization: Bearer t" || len(hook.Notes) == 0 {
		t.Errorf("webhook: %v %v", tc, hook.Notes)
	}
	for _, n := range d.Notifications {
		p, _ := notify.Get(n.Type)
		if _, err := p.Normalize(n.Config); err != nil {
			t.Errorf("%s bildirim ayarı geçersiz: %v (%s)", n.Name, err, n.Config)
		}
	}
}

// kumaJSONBackup Kuma 1.23 "Yedekle → Dışa aktar" çıktısının kısaltılmış hali
// (anahtar adları Kuma'nın monitor.toJSON() çıktısındaki gibi).
const kumaJSONBackup = `{
 "version": "1.23.16",
 "notificationList": [
  {"id": 1, "name": "Telegram", "config": "{\"name\":\"Telegram\",\"type\":\"telegram\",\"telegramBotToken\":\"123:ABC\",\"telegramChatID\":\"-100\",\"isDefault\":true}", "active": 1, "userId": 1, "isDefault": true},
  {"id": 2, "name": "E-posta", "config": "{\"type\":\"smtp\",\"smtpHost\":\"smtp.kadir.app\",\"smtpPort\":465,\"smtpSecure\":true,\"smtpUsername\":\"u\",\"smtpPassword\":\"p\",\"smtpFrom\":\"uptime@kadir.app\",\"smtpTo\":\"a@kadir.app\",\"smtpCC\":\"b@kadir.app\"}", "active": 1, "isDefault": false},
  {"id": 3, "name": "Telegram", "config": "{\"type\":\"telegram\",\"telegramBotToken\":\"456:DEF\",\"telegramChatID\":\"42\"}", "active": false, "isDefault": false},
  {"id": 4, "name": "Apprise", "config": "{\"type\":\"apprise\",\"appriseURL\":\"tgram://x\"}", "active": true},
  {"id": 5, "name": "Feishu", "config": "{\"type\":\"Feishu\"}", "active": true},
  {"id": 6, "name": "Kanca", "config": "{\"type\":\"webhook\",\"webhookURL\":\"https://hook.kadir.app\",\"webhookContentType\":\"custom\",\"webhookAdditionalHeaders\":\"{\\\"Authorization\\\":\\\"Bearer t\\\"}\"}", "active": true}
 ],
 "monitorList": [
  {"id": 1, "name": "Ana site", "description": "açıklama", "type": "http", "url": "https://kadir.app", "method": "POST",
   "body": "a=1", "headers": "{\"X-Token\":\"abc\",\"Accept\":\"text/html\"}", "basic_auth_user": "admin", "basic_auth_pass": "gizli",
   "authMethod": "basic", "accepted_statuscodes": ["200-299", "301"], "maxredirects": 3, "ignoreTls": true, "interval": 30,
   "retryInterval": 25, "maxretries": 3, "timeout": 12.5, "resendInterval": 5, "upsideDown": false, "active": true,
   "httpBodyEncoding": "form", "expiryNotification": false, "parent": null,
   "notificationIDList": {"1": true, "2": true, "4": true}, "tags": [{"tag_id": 1, "monitor_id": 1, "value": "canlı", "name": "ortam", "color": "#DC2626"}]},
  {"id": 2, "name": "Kelime", "type": "keyword", "url": "https://kadir.app/durum", "keyword": "Hata", "invertKeyword": true,
   "interval": 10, "active": false, "parent": 7, "notificationIDList": {}},
  {"id": 3, "name": "JSON", "type": "json-query", "url": "https://api.kadir.app/saglik", "jsonPath": "$.data.items[0].status",
   "expectedValue": "ok", "interval": 60, "active": true, "parent": 7},
  {"id": 4, "name": "SSH", "type": "port", "hostname": "sunucu.kadir.app", "port": 22, "interval": 60, "active": true,
   "notificationIDList": {"3": true}},
  {"id": 5, "name": "Ping", "type": "ping", "hostname": "1.1.1.1", "interval": 60, "active": true},
  {"id": 6, "name": "DNS", "type": "dns", "hostname": "kadir.app", "dns_resolve_server": "8.8.8.8", "dns_resolve_type": "MX",
   "port": 53, "interval": 60, "active": true},
  {"id": 7, "name": "Grup", "type": "group", "interval": 60, "active": true},
  {"id": 8, "name": "Yedek işi", "type": "push", "pushToken": "Kuma1234Token", "interval": 3600, "maxretries": 50, "active": true,
   "notificationIDList": {"6": true, "99": true}},
  {"id": 9, "name": "Konteyner", "type": "docker", "docker_container": "db", "interval": 60, "active": true},
  {"id": 10, "name": "Oyun", "type": "steam", "hostname": "oyun.kadir.app", "interval": 60, "active": true},
  {"id": 11, "name": "mTLS", "type": "http", "url": "https://ic.kadir.app", "authMethod": "mtls", "tlsCert": "CERT", "tlsKey": "KEY", "tlsCa": "CA", "interval": 60, "active": true},
  {"id": 12, "name": "OAuth", "type": "http", "url": "https://api.kadir.app", "authMethod": "oauth2-cc", "oauth_token_url": "https://auth.kadir.app/token",
   "oauth_client_id": "cid", "oauth_client_secret": "csecret", "oauth_scopes": "okuma", "oauth_auth_method": "client_secret_post", "interval": 60, "active": true}
 ]
}`

func TestKumaJSON(t *testing.T) {
	res, err := FromKumaJSON([]byte(kumaJSONBackup))
	if err != nil {
		t.Fatal(err)
	}
	checkKumaDoc(t, res)
	if len(res.Warnings) == 0 || !strings.Contains(res.Warnings[0], "1.23.16") {
		t.Errorf("sürüm uyarısı: %v", res.Warnings)
	}
	site := monitorByName(t, res.Doc, "Ana site")
	if len(site.Tags) != 1 || site.Tags[0] != (MonitorTag{"ortam", "canlı"}) || len(res.Doc.Tags) != 1 {
		t.Errorf("JSON etiketleri: %+v %+v", site.Tags, res.Doc.Tags)
	}

	for _, bad := range []string{`{}`, `[1,2]`, `{"monitorList": "x"}`} {
		if _, err := FromKumaJSON([]byte(bad)); err == nil {
			t.Errorf("geçersiz yedek kabul edildi: %s", bad)
		}
	}
}

func TestKumaDBNotKuma(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baska.db")
	db, _ := sql.Open("sqlite", "file:"+path)
	db.Exec("CREATE TABLE x (id INTEGER)")
	db.Close()
	if _, err := FromKumaDB(context.Background(), path); err == nil || !strings.Contains(err.Error(), "Uptime Kuma") {
		t.Errorf("Kuma olmayan veritabanı: %v", err)
	}
}

func TestKumaHelpers(t *testing.T) {
	for in, want := range map[string]string{"#ABC": "#aabbcc", "#123456": "#123456", "red": DefaultTagColor, "": DefaultTagColor} {
		if got := NormalizeColor(in); got != want {
			t.Errorf("renk %q: %q", in, got)
		}
	}
	for in, want := range map[string]string{"$.a.b": "a.b", "a[2].b": "a.2.b", "$": "", "$sum(x)": "$sum(x)"} {
		if got, _ := kumaJSONPath(in); got != want {
			t.Errorf("jsonpath %q: %q", in, got)
		}
	}
	used := map[string]bool{}
	if a, b, c := UniqueName("X", used), UniqueName("x", used), UniqueName("X", used); a != "X" || b != "x (2)" || c != "X (3)" {
		t.Errorf("benzersiz adlar: %s %s %s", a, b, c)
	}
}
