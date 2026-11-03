package notify

import (
	"github.com/kadirsungurlu/bekci/internal/i18n"
	"github.com/kadirsungurlu/bekci/internal/mesh"
)

// meshRows mesh bildiriminin satırları: mesaj sahibi, yorum, tüm bağlantılar.
func (e Event) meshRows() []Row {
	if e.Mesh == nil {
		return nil
	}
	l, r := e.Lang, e.Mesh
	rows := []Row{{Key: "mesh_owner", Label: i18n.T(l, "notify.field.mesh_owner"), Value: r.Names.In(l, r.Owner)}}
	verdict := Row{Key: "mesh_verdict", Label: i18n.T(l, "notify.field.mesh_verdict")}
	for _, s := range mesh.FindingLines(l, r.Verdict, r.Names) {
		verdict.Locations = append(verdict.Locations, LocRow{Name: s, OK: r.Verdict.Healthy()})
	}
	links := Row{Key: "mesh_links", Label: i18n.T(l, "notify.field.mesh_links")}
	for _, p := range r.Verdict.Pairs {
		links.Locations = append(links.Locations, LocRow{Name: mesh.PairText(l, p, r.Names),
			OK: p.State == mesh.PairUp, NoData: p.State == mesh.PairUnknown})
	}
	rows = append(rows, verdict)
	if len(links.Locations) > 0 {
		rows = append(rows, links)
	}
	return rows
}
