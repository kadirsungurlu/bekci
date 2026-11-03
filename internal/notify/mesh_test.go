package notify

import (
	"strings"
	"testing"
	"time"
)

func TestMeshBody(t *testing.T) {
	ev := Event{Kind: KindMesh, Lang: "tr", Time: time.Unix(0, 0), Mesh: sampleMesh()}
	body := ev.Body()
	for _, want := range []string{"Mesaj sahibi: Main", "• B genel erişim sorunu yaşıyor.", "• Main > A arası bağlantı sağlıklı",
		"• Main > B arası bağlantı koptu", "• A > B arası bağlantı koptu"} {
		if !strings.Contains(body, want) {
			t.Errorf("gövdede %q yok:\n%s", want, body)
		}
	}
	if ev.Title() != "Kontrol noktası bağlantı sorunu" || !ev.IsProblem() || ev.AlertKey() != "uptime-mesh" {
		t.Fatalf("başlık/anahtar: %q %q", ev.Title(), ev.AlertKey())
	}
	ok := Event{Kind: KindMeshResolved, Lang: "en", Time: time.Unix(0, 0), Downtime: time.Minute, Mesh: sampleMesh()}
	if !ok.IsRecovery() || ProblemOf(KindMeshResolved) != KindMesh || !strings.Contains(ok.Body(), "Reported by: Main") {
		t.Fatalf("düzelme: %s", ok.Body())
	}
}
