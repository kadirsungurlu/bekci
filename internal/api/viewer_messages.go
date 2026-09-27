package api

import (
	"encoding/json"
	"regexp"

	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
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

// viewerEvent canlı akıştaki "beat" olayının mesajını kullanıcıya göre
// temizler; diğer olaylar olduğu gibi döner. groups: grup monitörlerinin kimlikleri.
func viewerEvent(u store.User, msg []byte, groups map[int64]bool) []byte {
	if canSeeConfig(u) {
		return msg
	}
	var ev struct {
		Type string         `json:"type"`
		Data map[string]any `json:"data"`
	}
	if json.Unmarshal(msg, &ev) != nil || ev.Type != "beat" {
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
