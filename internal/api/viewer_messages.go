package api

import (
	"bytes"
	"encoding/json"
	"regexp"

	"github.com/kadirsungurlu/bekci/internal/i18n"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// İzleyicilere giden kontrol mesajlarının temizlenmesi.
//
// Kontrol mesajları hata metinleri içerir; bunlar hedef adresi (kullanıcı adı,
// sorgu parametresindeki token) ya da grup monitöründe alt monitörlerin
// adlarını barındırabilir. Editör ve yönetici her şeyi görür; izleyicide
// adreslerden kimlik bilgisi ve sorgu atılır, monitör kısıtlı izleyicide
// (müşteri) grup mesajı alt monitör adı içermeyen genel bir ifadeye çevrilir.

var messageURLRe = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^\s"'<>]+`)

// sanitizeMessage mesajdaki adreslerden kullanıcı adı/şifre, sorgu ve parçayı atar.
func sanitizeMessage(msg string) string {
	return messageURLRe.ReplaceAllStringFunc(msg, publicTarget)
}

// groupMessageFor alt monitör adı içermeyen grup durumu mesajı.
func groupMessageFor(status int) string {
	switch status {
	case store.StatusUp:
		return "Tüm alt monitörler çalışıyor"
	case store.StatusDown:
		return "Alt monitörlerden en az biri çalışmıyor"
	case store.StatusMaintenance:
		return "Bakımda"
	}
	return "Alt monitörler kontrol ediliyor"
}

// viewerMessage kullanıcının görebileceği mesajı döner.
func viewerMessage(u store.User, monitorType string, status int, msg string) string {
	if canSeeConfig(u) || msg == "" {
		return msg
	}
	if u.Restricted() && monitorType == "group" {
		return groupMessageFor(status)
	}
	return sanitizeMessage(msg)
}

// displayMessage kontrol mesajını kullanıcıya göre temizler (viewerMessage)
// ve yanıt diline çevirir. Mesajlar veritabanında Türkçe saklanır; çeviri
// okuma anındadır (bkz. i18n.Message). Temizlik Türkçe metin üzerinde yapılır.
func displayMessage(u store.User, lang, monitorType string, status int, msg string) string {
	return i18n.Message(lang, viewerMessage(u, monitorType, status, msg))
}

// localizeEvent canlı akış olayındaki Türkçe metinleri bağlantının diline
// çevirir: "beat" olayının mesajı ve "server" olayının ajan notu. Ortak
// yayın (hub) Türkçe kalır; çeviri abone başına, viewerEvent'ten sonra yapılır.
// tr bağlantılarda ve metin içermeyen olaylarda hiçbir şey çözülmez.
func localizeEvent(lang string, msg []byte) []byte {
	if i18n.Or(lang) == i18n.TR {
		return msg
	}
	if !bytes.Contains(msg, []byte(`"type":"beat"`)) && !bytes.Contains(msg, []byte(`"type":"server"`)) {
		return msg // ucuz ön eleme: diğer olaylar çözülmez
	}
	var ev struct {
		Type string                     `json:"type"`
		Data map[string]json.RawMessage `json:"data"`
	}
	if json.Unmarshal(msg, &ev) != nil || ev.Data == nil {
		return msg
	}
	var field string
	switch ev.Type {
	case "beat":
		field = "message"
	case "server":
		field = "note"
	default:
		return msg
	}
	var text string
	if raw, ok := ev.Data[field]; !ok || json.Unmarshal(raw, &text) != nil || text == "" {
		return msg
	}
	en := i18n.Message(lang, text)
	if en == text {
		return msg
	}
	b, err := json.Marshal(en)
	if err != nil {
		return msg
	}
	ev.Data[field] = b
	out, err := json.Marshal(ev)
	if err != nil {
		return msg
	}
	return out
}

// viewerEvent canlı akıştaki "beat" olayının mesajını kullanıcıya göre
// temizler; "server" olayından yönetici olmayan kullanıcı için IP kilidi
// bilgisini (sabitlenmiş IP) atar. Diğer olaylar olduğu gibi döner. groups:
// grup monitörlerinin kimlikleri.
func viewerEvent(u store.User, msg []byte, groups map[int64]bool) []byte {
	if isPageAdmin(u) {
		return msg
	}
	if canSeeConfig(u) && !bytes.Contains(msg, []byte(`"locked_ip"`)) {
		return msg
	}
	var ev struct {
		Type string         `json:"type"`
		Data map[string]any `json:"data"`
	}
	if json.Unmarshal(msg, &ev) != nil {
		return msg
	}
	if ev.Type == "server" {
		if ev.Data == nil {
			return msg
		}
		if _, ok := ev.Data["locked_ip"]; ok {
			ev.Data["locked_ip"] = ""
		}
		if _, ok := ev.Data["ip_lock"]; ok {
			ev.Data["ip_lock"] = false
		}
		out, err := json.Marshal(ev)
		if err != nil {
			return nil // güvenli taraf: IP'li hali gönderilmez
		}
		return out
	}
	if ev.Type != "beat" || canSeeConfig(u) {
		return msg
	}
	text, _ := ev.Data["message"].(string)
	if text == "" {
		return msg
	}
	id, _ := ev.Data["monitor_id"].(float64)
	status, _ := ev.Data["status"].(float64)
	typ := ""
	if groups[int64(id)] {
		typ = "group"
	}
	ev.Data["message"] = viewerMessage(u, typ, int(status), text)
	out, err := json.Marshal(ev)
	if err != nil {
		return msg
	}
	return out
}
