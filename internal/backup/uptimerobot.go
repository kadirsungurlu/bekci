package backup

// UptimeRobot'tan içe aktarma: v2 API'sinin getMonitors uç noktası salt okunur
// API anahtarıyla çağrılır ve monitörler bu uygulamanın biçimine çevrilir.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// UptimeRobotAPI testlerde sahte sunucuya yönlendirmek için değiştirilebilir.
var UptimeRobotAPI = "https://api.uptimerobot.com/v2"

const (
	urPageSize = 50
	urMaxPages = 100 // en fazla 5000 monitör
)

// ErrUptimeRobot UptimeRobot'un döndürdüğü hata (anahtar geçersiz vb.); ağ
// hatalarından ayırt etmek için.
type ErrUptimeRobot struct{ Msg string }

func (e *ErrUptimeRobot) Error() string { return "UptimeRobot hatası: " + e.Msg }

// URMonitor getMonitors yanıtındaki monitör (kullanılan alanlar).
type URMonitor struct {
	ID               int64           `json:"id"`
	FriendlyName     string          `json:"friendly_name"`
	URL              string          `json:"url"`
	Type             int             `json:"type"`
	SubType          json.RawMessage `json:"sub_type"`
	KeywordType      json.RawMessage `json:"keyword_type"`
	KeywordCaseType  json.RawMessage `json:"keyword_case_type"`
	KeywordValue     string          `json:"keyword_value"`
	HTTPUsername     string          `json:"http_username"`
	HTTPPassword     string          `json:"http_password"`
	HTTPMethod       json.RawMessage `json:"http_method"`
	PostValue        json.RawMessage `json:"post_value"`
	Port             json.RawMessage `json:"port"`
	Interval         int             `json:"interval"`
	Timeout          int             `json:"timeout"`
	Status           int             `json:"status"`
	CustomHTTPHeader json.RawMessage `json:"custom_http_headers"`
}

// FetchUptimeRobot tüm monitörleri sayfa sayfa çeker.
func FetchUptimeRobot(ctx context.Context, client *http.Client, apiKey string) ([]URMonitor, error) {
	var all []URMonitor
	for page := 0; page < urMaxPages; page++ {
		form := url.Values{
			"api_key": {apiKey}, "format": {"json"}, "alert_contacts": {"0"}, "custom_http_headers": {"1"},
			"offset": {fmt.Sprint(page * urPageSize)}, "limit": {fmt.Sprint(urPageSize)},
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, UptimeRobotAPI+"/getMonitors", strings.NewReader(form.Encode()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Cache-Control", "no-cache")
		req.Header.Set("User-Agent", "Bekci (+https://bekci.app)")
		resp, err := client.Do(req)
		if err != nil {
			var ue *url.Error
			if errors.As(err, &ue) {
				err = ue.Err // adres ve anahtar mesaja girmesin
			}
			return nil, fmt.Errorf("UptimeRobot'a bağlanılamadı: %v", err)
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("UptimeRobot yanıtı okunamadı: %v", err)
		}
		var body struct {
			Stat  string `json:"stat"`
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
			Pagination struct {
				Offset int `json:"offset"`
				Limit  int `json:"limit"`
				Total  int `json:"total"`
			} `json:"pagination"`
			Monitors []URMonitor `json:"monitors"`
		}
		if err := json.Unmarshal(data, &body); err != nil {
			if resp.StatusCode != http.StatusOK {
				return nil, &ErrUptimeRobot{fmt.Sprintf("HTTP %d", resp.StatusCode)}
			}
			return nil, &ErrUptimeRobot{"yanıt çözülemedi"}
		}
		if body.Stat != "ok" {
			msg := strings.TrimSpace(body.Error.Message)
			if msg == "" {
				msg = strings.TrimSpace(body.Error.Type + " " + fmt.Sprint(resp.StatusCode))
			}
			return nil, &ErrUptimeRobot{msg}
		}
		all = append(all, body.Monitors...)
		if len(body.Monitors) == 0 || len(all) >= body.Pagination.Total {
			return all, nil
		}
	}
	return all, nil
}

// rawInt sayı, sayısal metin veya null olabilen alanı okur.
func rawInt(raw json.RawMessage) int {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return 0
	}
	return kv{"v": v}.int("v")
}

func rawStr(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	return kv{"v": v}.str("v")
}

var urPorts = map[int]int{1: 80, 2: 443, 3: 21, 4: 25, 5: 110, 6: 143}

var urMethods = map[int]string{1: "HEAD", 2: "GET", 3: "POST", 4: "PUT", 5: "PATCH", 6: "DELETE", 7: "OPTIONS"}

// FromUptimeRobot UptimeRobot monitörlerini çevirir.
func FromUptimeRobot(list []URMonitor) *Result {
	res := newResult()
	res.Warnings = append(res.Warnings,
		"Uyarı kişileri (alert contacts), bakım pencereleri ve geçmiş aktarılmaz; bildirim kanallarını bu uygulamada ayrıca bağlayın.")
	hasHeartbeat := false
	for _, u := range list {
		name := strings.TrimSpace(u.FriendlyName)
		m := Monitor{
			ID: u.ID, Name: name, Active: u.Status != 0, Notifications: []string{}, Tags: []MonitorTag{},
			Description: "UptimeRobot'tan aktarıldı",
		}
		if r := []rune(m.Name); len(r) > 100 {
			m.Name = string(r[:100])
		}
		interval := u.Interval
		if interval <= 0 {
			interval = 300
		}
		m.Interval = clamp(interval, 20, 86400)
		m.RetryInterval = m.Interval
		m.Timeout = 30
		if u.Timeout > 0 {
			m.Timeout = clamp(u.Timeout, 1, 300)
		}
		// UptimeRobot kesinti bildirmeden önce kısa bir doğrulama yapar; buna en
		// yakın davranış tek bir tekrar denemesidir.
		m.MaxRetries = 1
		target := strings.TrimSpace(u.URL)
		var cfg map[string]any
		switch u.Type {
		case 1, 2: // HTTP(s), anahtar kelime
			m.Type = "http"
			cfg = map[string]any{"url": target, "method": "GET", "accepted_codes": []string{"200-399"}}
			if meth := urMethods[rawInt(u.HTTPMethod)]; meth != "" && meth != "HEAD" && meth != "GET" {
				cfg["method"] = meth
				if body := rawStr(u.PostValue); body != "" {
					cfg["body"] = body
				}
			}
			if u.HTTPUsername != "" || u.HTTPPassword != "" {
				cfg["basic_user"], cfg["basic_pass"] = u.HTTPUsername, u.HTTPPassword
			}
			if h := urHeaders(u.CustomHTTPHeader); h != "" {
				cfg["headers"] = h
			}
			if u.Type == 2 {
				cfg["keyword"] = u.KeywordValue
				// keyword_type 1: "kelime VARSA uyar" → kelime bulunursa DOWN.
				cfg["keyword_invert"] = rawInt(u.KeywordType) == 1
				cfg["keyword_case"] = rawInt(u.KeywordCaseType) == 1
			}
		case 3: // ping
			m.Type = "ping"
			cfg = map[string]any{"host": target}
		case 4: // port
			m.Type = "tcp"
			port := urPorts[rawInt(u.SubType)]
			if port == 0 {
				port = rawInt(u.Port)
			}
			cfg = map[string]any{"host": target, "port": port}
		case 5: // heartbeat
			m.Type = "push"
			cfg = map[string]any{}
			hasHeartbeat = true
			m.Notes = append(m.Notes, "Yeni push adresi oluşturuldu; cron işindeki UptimeRobot adresini güncelleyin")
		default:
			res.skip("monitor", name, fmt.Sprint(u.Type), "Bu monitör tipi desteklenmiyor")
			continue
		}
		if m.Interval != interval {
			m.Notes = append(m.Notes, fmt.Sprintf("Kontrol aralığı %d sn → %d sn", interval, m.Interval))
		}
		m.Config, _ = json.Marshal(cfg)
		res.Doc.Monitors = append(res.Doc.Monitors, m)
	}
	if hasHeartbeat {
		res.Warnings = append(res.Warnings,
			"Heartbeat monitörleri için yeni push adresleri oluşturuldu; cron işlerinizdeki UptimeRobot adreslerini yenileriyle değiştirin.")
	}
	return res
}

func urHeaders(raw json.RawMessage) string {
	var obj map[string]any
	if json.Unmarshal(raw, &obj) != nil || len(obj) == 0 {
		return ""
	}
	b, _ := json.Marshal(obj)
	h, _ := kumaHeaders(string(b))
	return h
}
