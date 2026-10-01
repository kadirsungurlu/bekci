package notify

import (
	"bytes"
	"html/template"
	"strings"

	"github.com/kadirsungurlu/bekci/internal/brand"
	"github.com/kadirsungurlu/bekci/internal/i18n"
)

// E-posta HTML gövdesi: e-posta istemcileri için tablo düzeni ve satır içi
// stil (çoğu istemci <style> ve modern CSS'i yok sayar). Düz metin sürümü
// (Text) aynı iletide alternatif olarak gider.

type mailTone struct{ Accent, Soft, Ink string }

var (
	toneDown = mailTone{"#dc2626", "#fef2f2", "#991b1b"}
	toneUp   = mailTone{"#059669", "#ecfdf5", "#065f46"}
	toneWarn = mailTone{"#d97706", "#fffbeb", "#92400e"}
	toneInfo = mailTone{"#475569", "#f1f5f9", "#334155"}
)

type mailView struct {
	Lang, Brand, Status, Heading, Link, Open, Footer string
	Tone                                             mailTone
	Notes                                            []string
	Rows                                             []Row
}

func (e Event) mailStatus() (string, mailTone) {
	key, tone := "test", toneInfo
	switch e.Kind {
	case KindDown:
		key, tone = "down", toneDown
	case KindUp:
		key, tone = "up", toneUp
	case KindReminder:
		key, tone = "reminder", toneDown
	case KindCert:
		key, tone = "cert", toneWarn
	case KindServerAlert:
		key, tone = "alert", toneDown
	case KindServerResolved:
		key, tone = "resolved", toneUp
	}
	return i18n.T(e.Lang, "notify.mail.status."+key), tone
}

// plainTitle başlığın baştaki emojisiz hali (HTML'de durum rozeti var).
func plainTitle(s string) string {
	return strings.TrimSpace(strings.TrimLeft(s, "🔴🟢⚠️✅ "))
}

// HTML e-posta gövdesi.
func (e Event) HTML() string {
	status, tone := e.mailStatus()
	v := mailView{
		Lang: e.Lang, Brand: brand.Name, Status: status, Heading: plainTitle(e.Title()),
		Link: e.DetailURL(), Open: i18n.T(e.Lang, "notify.mail.open"),
		Footer: i18n.T(e.Lang, "notify.mail.footer", brand.Name), Tone: tone, Notes: e.Notes(),
	}
	for _, r := range e.Rows() {
		if r.Key != "link" {
			v.Rows = append(v.Rows, r)
		}
	}
	if v.Lang == "" {
		v.Lang = "tr"
	}
	var b bytes.Buffer
	if err := mailTmpl.Execute(&b, v); err != nil {
		return ""
	}
	return b.String()
}

var mailTmpl = template.Must(template.New("mail").Parse(`<!DOCTYPE html>
<html lang="{{.Lang}}"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="color-scheme" content="light"><title>{{.Heading}}</title></head>
<body style="margin:0;padding:0;background:#f3f4f6;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;color:#111827">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:#f3f4f6"><tr><td align="center" style="padding:28px 12px">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width:560px">
<tr><td style="padding:0 4px 12px;font-size:15px;font-weight:700;color:#111827"><span style="display:inline-block;width:10px;height:10px;border-radius:5px;background:#10b981;margin-right:7px;vertical-align:1px"></span>{{.Brand}}</td></tr>
<tr><td style="background:#ffffff;border:1px solid #e5e7eb;border-top:4px solid {{.Tone.Accent}};border-radius:10px;padding:24px 24px 22px">
<span style="display:inline-block;padding:3px 10px;border-radius:999px;background:{{.Tone.Soft}};color:{{.Tone.Ink}};font-size:12px;font-weight:700;letter-spacing:.02em">{{.Status}}</span>
<h1 style="margin:12px 0 0;font-size:20px;line-height:1.35;font-weight:700;color:#111827">{{.Heading}}</h1>
{{range .Notes}}<p style="margin:10px 0 0;font-size:14px;line-height:1.5;color:#4b5563">{{.}}</p>{{end}}
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="margin-top:18px;border-top:1px solid #f1f5f9">
{{range .Rows}}<tr>
<td valign="top" style="padding:10px 12px 10px 0;width:104px;font-size:13px;color:#6b7280;border-bottom:1px solid #f1f5f9">{{.Label}}</td>
<td valign="top" style="padding:10px 0;font-size:14px;line-height:1.5;color:#111827;border-bottom:1px solid #f1f5f9;word-break:break-word">{{if .Locations}}{{range .Locations}}<div style="padding:3px 0 5px"><div><span style="display:inline-block;width:8px;height:8px;border-radius:4px;background:{{if .NoData}}#9ca3af{{else}}#dc2626{{end}};margin-right:8px;vertical-align:1px"></span><b style="font-weight:600">{{.Name}}</b></div><div style="padding-left:16px;font-size:13px;color:{{if .NoData}}#6b7280{{else}}#4b5563{{end}}">{{.Message}}</div></div>{{end}}{{else}}{{.Value}}{{end}}</td>
</tr>{{end}}
</table>
{{if .Link}}<table role="presentation" cellpadding="0" cellspacing="0" style="margin-top:22px"><tr><td style="border-radius:8px;background:#10b981"><a href="{{.Link}}" style="display:inline-block;padding:11px 20px;font-size:14px;font-weight:700;color:#022c22;text-decoration:none">{{.Open}}</a></td></tr></table>{{end}}
</td></tr>
<tr><td style="padding:14px 4px 0;font-size:12px;line-height:1.5;color:#9ca3af">{{.Footer}}{{if .Link}}<br><a href="{{.Link}}" style="color:#9ca3af">{{.Link}}</a>{{end}}</td></tr>
</table></td></tr></table></body></html>`))
