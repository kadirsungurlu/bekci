package backup

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"unicode"

	"github.com/kadirsa1105/uptime-kadir-app/internal/notify"
)

// kumaNotifier bir Kuma bildirim türünün bu uygulamadaki karşılığı.
type kumaNotifier struct {
	typ  string
	conv func(c kv) (map[string]any, []string)
}

// kumaNotifiers anahtarları Kuma tür adlarının sadeleştirilmiş hali (küçük
// harf, yalnızca harf/rakam): "rocket.chat" → "rocketchat", "GoogleChat" →
// "googlechat". Alan adları Kuma'nın notification.config JSON'undakilerdir.
var kumaNotifiers = map[string]kumaNotifier{
	"telegram": {"telegram", func(c kv) (map[string]any, []string) {
		return map[string]any{"bot_token": c.str("telegramBotToken"), "chat_id": c.str("telegramChatID"),
			"thread_id": c.int("telegramMessageThreadID")}, nil
	}},
	"discord": {"discord", func(c kv) (map[string]any, []string) {
		return map[string]any{"webhook_url": c.str("discordWebhookUrl")}, nil
	}},
	"slack": {"slack", func(c kv) (map[string]any, []string) {
		return map[string]any{"webhook_url": c.str("slackwebhookURL", "slackWebhookURL")}, nil
	}},
	"teams": {"teams", func(c kv) (map[string]any, []string) {
		return map[string]any{"webhook_url": c.str("webhookUrl")}, nil
	}},
	"googlechat": {"googlechat", func(c kv) (map[string]any, []string) {
		return map[string]any{"webhook_url": c.str("googleChatWebhookURL")}, nil
	}},
	"webhook": {"webhook", func(c kv) (map[string]any, []string) {
		var notes []string
		headers, note := kumaHeaders(c.str("webhookAdditionalHeaders"))
		if note != "" {
			notes = append(notes, note)
		}
		if ct := c.str("webhookContentType"); ct != "" && ct != "json" {
			notes = append(notes, "Özel gövde/form biçimi aktarılmadı; bu uygulama kendi JSON gövdesini gönderir")
		}
		return map[string]any{"url": c.str("webhookURL"), "method": "POST", "headers": headers}, notes
	}},
	"smtp": {"email", func(c kv) (map[string]any, []string) {
		var notes []string
		security := "starttls"
		if c.bool("smtpSecure") {
			security = "tls"
		} else if c.str("smtpUsername") == "" && c.int("smtpPort") == 25 {
			security = "none"
		}
		to := joinCSV(c.str("smtpTo"), c.str("smtpCC"), c.str("smtpBCC"))
		if c.str("smtpCC") != "" || c.str("smtpBCC") != "" {
			notes = append(notes, "CC/BCC alıcıları doğrudan alıcı listesine eklendi")
		}
		if c.str("customSubject") != "" || c.str("customBody") != "" {
			notes = append(notes, "Özel konu/gövde şablonu aktarılmadı")
		}
		return map[string]any{"host": c.str("smtpHost"), "port": c.int("smtpPort"), "security": security,
			"username": c.str("smtpUsername"), "password": c.str("smtpPassword"), "from": c.str("smtpFrom"), "to": to}, notes
	}},
	"ntfy": {"ntfy", func(c kv) (map[string]any, []string) {
		var notes []string
		cfg := map[string]any{"server": c.str("ntfyserverurl"), "topic": c.str("ntfytopic")}
		if p := c.int("ntfyPriority"); p > 0 {
			cfg["priority"] = clamp(p, 1, 5)
		}
		if t := c.str("ntfyaccesstoken"); t != "" {
			cfg["token"] = t
		} else if c.str("ntfyusername") != "" {
			notes = append(notes, "ntfy kullanıcı adı/şifre desteklenmiyor; erişim token'ı girin")
		}
		return cfg, notes
	}},
	"gotify": {"gotify", func(c kv) (map[string]any, []string) {
		cfg := map[string]any{"server": c.str("gotifyserverurl"), "app_token": c.str("gotifyapplicationToken")}
		if p := c.int("gotifyPriority"); p > 0 {
			cfg["priority"] = clamp(p, 1, 10)
		}
		return cfg, nil
	}},
	"pushover": {"pushover", func(c kv) (map[string]any, []string) {
		return map[string]any{"user_key": c.str("pushoveruserkey"), "app_token": c.str("pushoverapptoken"),
			"device": c.str("pushoverdevice"), "priority": clamp(c.int("pushoverpriority"), -2, 1)}, nil
	}},
	"matrix": {"matrix", func(c kv) (map[string]any, []string) {
		return map[string]any{"homeserver_url": c.str("homeserverUrl"), "access_token": c.str("accessToken"),
			"room_id": c.str("internalRoomId")}, nil
	}},
	"mattermost": {"mattermost", func(c kv) (map[string]any, []string) {
		cfg := map[string]any{"webhook_url": c.str("mattermostWebhookUrl"), "channel": c.str("mattermostchannel"),
			"username": c.str("mattermostusername")}
		if u := c.str("mattermosticonurl"); strings.HasPrefix(u, "http") {
			cfg["icon_url"] = u
		}
		return cfg, nil
	}},
	"rocketchat": {"rocketchat", func(c kv) (map[string]any, []string) {
		return map[string]any{"webhook_url": c.str("rocketwebhookURL"), "channel": c.str("rocketchannel"),
			"alias": c.str("rocketusername")}, nil
	}},
	"pagerduty": {"pagerduty", func(c kv) (map[string]any, []string) {
		sev := strings.ToLower(c.str("pagerdutyPriority"))
		if sev != "critical" && sev != "error" && sev != "warning" && sev != "info" {
			sev = "critical"
		}
		var notes []string
		if u := c.str("pagerdutyIntegrationUrl"); u != "" && !strings.Contains(u, "events.pagerduty.com") {
			notes = append(notes, "Özel PagerDuty adresi aktarılmadı")
		}
		return map[string]any{"routing_key": c.str("pagerdutyIntegrationKey"), "severity": sev}, notes
	}},
	"opsgenie": {"opsgenie", func(c kv) (map[string]any, []string) {
		prio := "P3"
		if p := c.int("opsgeniePriority"); p >= 1 && p <= 5 {
			prio = fmt.Sprintf("P%d", p)
		}
		return map[string]any{"api_key": c.str("opsgenieApiKey"), "region": strings.ToLower(c.str("opsgenieRegion")),
			"priority": prio}, nil
	}},
	"homeassistant": {"homeassistant", func(c kv) (map[string]any, []string) {
		svc := strings.TrimPrefix(strings.TrimSpace(c.str("notificationService")), "notify.")
		return map[string]any{"url": c.str("homeAssistantUrl"), "token": c.str("longLivedAccessToken"), "service": svc}, nil
	}},
	"signal": {"signal", func(c kv) (map[string]any, []string) {
		return map[string]any{"url": c.str("signalURL"), "number": c.str("signalNumber"),
			"recipients": c.str("signalRecipients")}, nil
	}},
	"twilio": {"twilio", func(c kv) (map[string]any, []string) {
		var notes []string
		if c.str("twilioApiKey") != "" {
			notes = append(notes, "Twilio API anahtarıyla giriş desteklenmiyor; Auth Token kullanılır")
		}
		return map[string]any{"account_sid": c.str("twilioAccountSID"), "auth_token": c.str("twilioAuthToken"),
			"from": c.str("twilioFromNumber"), "to": c.str("twilioToNumber")}, notes
	}},
	"pushbullet": {"pushbullet", func(c kv) (map[string]any, []string) {
		return map[string]any{"access_token": c.str("pushbulletAccessToken")}, nil
	}},
	"bark": {"bark", func(c kv) (map[string]any, []string) {
		// Kuma: barkEndpoint = "https://api.day.app/<cihaz anahtarı>"
		server, key := c.str("barkEndpoint"), ""
		if u, err := url.Parse(strings.TrimRight(server, "/")); err == nil && u.Host != "" {
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			key = parts[len(parts)-1]
			u.Path = strings.TrimSuffix(strings.TrimRight(u.Path, "/"), "/"+key)
			server = u.String()
		}
		return map[string]any{"server": server, "device_key": key, "group": c.str("barkGroup"), "sound": c.str("barkSound")}, nil
	}},
	"line": {"line", func(c kv) (map[string]any, []string) {
		return map[string]any{"channel_access_token": c.str("lineChannelAccessToken"), "to": c.str("lineUserID")}, nil
	}},
}

func joinCSV(parts ...string) string {
	var out []string
	for _, p := range parts {
		for _, s := range strings.Split(p, ",") {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
	}
	return strings.Join(out, ", ")
}

func simplifyType(t string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, t)
}

// convertKumaNotification Kuma bildirimini çevirir. reason boş değilse kanal atlanır.
func convertKumaNotification(kumaType string, c kv) (typ string, cfg map[string]any, notes []string, reason string) {
	key := simplifyType(kumaType)
	if key == "apprise" {
		return "", nil, nil, "Uptime Kuma'daki Apprise yerel komutla çalışır; burada Apprise API sunucusu gerekir, kanalı elle ekleyin"
	}
	n, ok := kumaNotifiers[key]
	if !ok {
		return "", nil, nil, "Bu bildirim türü desteklenmiyor"
	}
	if _, ok := notify.Get(n.typ); !ok {
		return "", nil, nil, "Bu bildirim türü bu sürümde yok"
	}
	cfg, notes = n.conv(c)
	// Boş metin alanları gönderilmez: sağlayıcının varsayılanı kullanılsın.
	for k, v := range cfg {
		if s, ok := v.(string); ok && strings.TrimSpace(s) == "" {
			delete(cfg, k)
		}
	}
	return n.typ, cfg, notes, ""
}

// KumaNotificationTypes eşlenen Kuma bildirim türleri (dokümantasyon ve testler için).
func KumaNotificationTypes() []string {
	out := make([]string, 0, len(kumaNotifiers))
	for k := range kumaNotifiers {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
