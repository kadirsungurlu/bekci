package i18n

// Mesh (kontrol noktaları arası bağlantı) metinleri; %[1]s / %[2]s düğüm adları.
func init() {
	for k, v := range map[string]Msg{
		"mesh.title":                {"Kontrol noktası bağlantı sorunu", "Check location connectivity problem"},
		"mesh.title.resolved":       {"Kontrol noktası bağlantıları düzeldi", "Check location connectivity restored"},
		"mesh.cause":                {"Kontrol noktası bağlantı sorunu: %s", "Check location connectivity problem: %s"},
		"mesh.pair.up":              {"%s > %s arası bağlantı sağlıklı", "%s > %s link healthy"},
		"mesh.pair.down":            {"%s > %s arası bağlantı koptu", "%s > %s link down"},
		"mesh.pair.oneway":          {"%s > %s arası tek yönlü (%s > %s çalışıyor)", "%s > %s one-way (%s > %s works)"},
		"mesh.pair.unknown":         {"%s > %s arası ölçülemiyor", "%s > %s not measured"},
		"mesh.finding.partition":    {"Ağ bölündü: %s", "Network partitioned: %s"},
		"mesh.finding.node":         {"%s genel erişim sorunu yaşıyor.", "%s has a general reachability problem."},
		"mesh.finding.link":         {"%s > %s arası bağlantı koptu (yol sorunu).", "%s > %s link down (path problem)."},
		"mesh.finding.oneway":       {"%s > %s tek yönlü: %s > %s kopuk.", "%s > %s one-way: %s > %s is down."},
		"mesh.finding.insufficient": {"%s > %s arası bağlantı koptu; yorum için yetersiz gözlem.", "%s > %s link down; not enough observations to interpret."},
		"mesh.incident.name":        {"Kontrol noktası bağlantıları", "Check location links"},
		"mesh.healthy":              {"Tüm bağlantılar sağlıklı.", "All links healthy."},
		"notify.field.mesh_owner":   {"Mesaj sahibi", "Reported by"},
		"notify.field.mesh_verdict": {"Yorum", "Interpretation"},
		"notify.field.mesh_links":   {"Bağlantılar", "Links"},
	} {
		messages[k] = v
	}
}
