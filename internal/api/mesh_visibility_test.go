package api

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// Mesh olayı ve bağlantı matrisi yalnız yöneticiye: düzenleyici/izleyici
// olaya güncelleme yazamaz, canlı akışta mesh olaylarını almaz, metriklerde
// mesh olay sayısını görmez.
func TestMeshHiddenFromNonAdmins(t *testing.T) {
	f := newServersEnv(t)
	admin := f.env
	ctx := context.Background()
	id, opened, err := f.st.OpenMeshIncident(ctx, 100, "mesh", `{}`)
	if err != nil || !opened {
		t.Fatal(err)
	}
	path := "/api/incidents/" + strconv.FormatInt(id, 10)
	editor, _ := admin.newUser("duzenleyici", store.RoleEditor, nil)
	viewer, _ := admin.newUser("izleyici", store.RoleViewer, nil)
	upd := map[string]any{"state": "investigating", "body": "not"}
	editor.mustDo("POST", path+"/updates", upd, nil, 404)
	editor.mustDo("POST", path+"/ack", map[string]any{}, nil, 404)
	editor.mustDo("GET", path, nil, nil, 404)
	viewer.mustDo("GET", path, nil, nil, 404)
	viewer.mustDo("GET", "/api/mesh", nil, nil, 403)
	admin.mustDo("POST", path+"/updates", upd, nil, 201)
	ups, err := f.st.IncidentUpdates(ctx, id)
	if err != nil || len(ups) != 1 {
		t.Fatalf("yönetici güncellemesi: %v %+v", err, ups)
	}
	editor.mustDo("DELETE", path+"/updates/"+strconv.FormatInt(ups[0].ID, 10), nil, nil, 404)

	// Canlı akış.
	meshMsg, _ := json.Marshal(map[string]any{"type": "mesh", "data": map[string]any{"links": []store.ProbeLink{{Src: 1, Dst: 2}}}})
	incMsg, _ := json.Marshal(map[string]any{"type": "incident", "data": map[string]any{"incident_id": id, "kind": store.IncidentMesh}})
	monMsg, _ := json.Marshal(map[string]any{"type": "incident", "data": map[string]any{"incident_id": 9, "kind": store.IncidentMonitor}})
	for _, role := range []string{store.RoleViewer, store.RoleEditor} {
		u := store.User{Role: role}
		if out := viewerEvent(u, meshMsg, nil); out != nil {
			t.Errorf("%s mesh olayını almamalı: %s", role, out)
		}
		if out := viewerEvent(u, incMsg, nil); out != nil {
			t.Errorf("%s mesh olay bildirimini almamalı: %s", role, out)
		}
		if out := viewerEvent(u, monMsg, nil); out == nil {
			t.Errorf("%s monitör olay bildirimini almalı", role)
		}
	}
	if viewerEvent(store.User{Role: store.RoleAdmin}, meshMsg, nil) == nil {
		t.Error("yönetici mesh olayını almalı")
	}

	// Metrikler.
	metric := func(role string) string {
		k := admin.newKey("prom-"+role, role, 0)
		code, _, body := admin.rawReq("GET", "/metrics", bearer(k.Secret), nil)
		if code != 200 {
			t.Fatalf("metrik %s: %d", role, code)
		}
		return string(body)
	}
	if strings.Contains(metric(store.RoleViewer), `kind="mesh"`) {
		t.Error("izleyici metriklerde mesh olay sayısını görmemeli")
	}
	if !strings.Contains(metric(store.RoleAdmin), `uptime_incidents_open{kind="mesh"} 1`) {
		t.Error("yönetici mesh olay sayısını görmeli")
	}
}
