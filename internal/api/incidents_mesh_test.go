package api

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/kadirsungurlu/bekci/internal/i18n"
	"github.com/kadirsungurlu/bekci/internal/mesh"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// Mesh olayı: yönetici ayrıntıda son yorumu ve bağlantıları görür (dilde);
// kısıtlı kullanıcı göremez.
func TestMeshIncidentDetail(t *testing.T) {
	f := newServersEnv(t)
	admin := f.env
	ctx := context.Background()
	v := mesh.Evaluate([]int64{0, 1, 2}, []mesh.Edge{{Src: 0, Dst: 1, State: "up"}, {Src: 0, Dst: 2, State: "down"}, {Src: 1, Dst: 2, State: "down"}})
	data, _ := json.Marshal(mesh.Report{Verdict: v, Names: mesh.Names{0: "Main", 1: "A", 2: "B"}})
	id, opened, err := f.st.OpenMeshIncident(ctx, 100, store.MeshIncidentCause(i18n.TR, string(data)), string(data))
	if err != nil || !opened {
		t.Fatal(err)
	}
	var out struct {
		Incident store.Incident `json:"incident"`
		Mesh     struct {
			Findings []string `json:"findings"`
			Nodes    []struct {
				ID   int64
				Name string
			} `json:"nodes"`
			Pairs []struct {
				State, Text, AB, BA string
			} `json:"pairs"`
		} `json:"mesh"`
		Events []store.IncidentEvent `json:"events"`
	}
	admin.mustDo("GET", "/api/incidents/"+strconv.FormatInt(id, 10), nil, &out, 200)
	if out.Incident.Cause != "Kontrol noktası bağlantı sorunu: B genel erişim sorunu yaşıyor." ||
		len(out.Mesh.Findings) != 1 || len(out.Mesh.Pairs) != 3 || out.Mesh.Pairs[0].Text != "Main > A arası bağlantı sağlıklı" ||
		len(out.Mesh.Nodes) != 3 || out.Mesh.Nodes[2].Name != "B" || out.Mesh.Pairs[0].AB == "" {
		t.Fatalf("ayrıntı: %+v", out)
	}
	if len(out.Events) == 0 || !strings.Contains(out.Events[len(out.Events)-1].Message, "B genel erişim") {
		t.Fatalf("işlem geçmişi: %+v", out.Events)
	}
	var list []store.Incident
	admin.mustDo("GET", "/api/incidents?kind=server", nil, &list, 200)
	if len(list) != 1 || list[0].Kind != store.IncidentMesh {
		t.Fatalf("süzgeç: %+v", list)
	}
	admin.mustDo("POST", "/api/users", map[string]any{"username": "izleyici", "role": "viewer", "password": "gecici-sifre-1"}, nil, 201)
	viewer := admin.loginAs("izleyici", "gecici-sifre-1")
	viewer.mustDo("POST", "/api/auth/password", map[string]string{"current": "gecici-sifre-1", "new": "kalici-sifre-1"}, nil, 200)
	viewer.mustDo("GET", "/api/incidents/"+strconv.FormatInt(id, 10), nil, nil, 404)
	var seen []store.Incident
	viewer.mustDo("GET", "/api/incidents", nil, &seen, 200)
	if len(seen) != 0 {
		t.Fatalf("izleyici mesh olayını listede görmemeli: %+v", seen)
	}
}
