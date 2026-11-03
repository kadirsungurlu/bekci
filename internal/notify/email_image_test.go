package notify

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/mesh"
)

type mailPart struct {
	ctype, cid, disp, body string
	children               []mailPart
}

// parseMail iletiyi (iç içe multipart dahil) ayrıştırır; gövdeler çözülmüş.
func parseMail(t *testing.T, raw []byte) mailPart {
	t.Helper()
	m, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return readPart(t, m.Header.Get("Content-Type"), "", "", "", m.Body)
}

func readPart(t *testing.T, ctype, enc, cid, disp string, r io.Reader) mailPart {
	t.Helper()
	mt, params, err := mime.ParseMediaType(ctype)
	if err != nil {
		t.Fatalf("içerik türü %q: %v", ctype, err)
	}
	p := mailPart{ctype: mt, cid: cid, disp: disp}
	if strings.HasPrefix(mt, "multipart/") {
		mr := multipart.NewReader(r, params["boundary"])
		for {
			c, err := mr.NextRawPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			p.children = append(p.children, readPart(t, c.Header.Get("Content-Type"), c.Header.Get("Content-Transfer-Encoding"), c.Header.Get("Content-ID"), c.Header.Get("Content-Disposition"), c))
		}
		return p
	}
	switch enc {
	case "quoted-printable":
		r = quotedprintable.NewReader(r)
	case "base64":
		r = base64.NewDecoder(base64.StdEncoding, r)
	}
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	p.body = string(b)
	return p
}

func meshMailEvent(t *testing.T) Event {
	v := mesh.Verdict{Nodes: []int64{0, 1, 2}, Pairs: []mesh.Pair{
		{A: 0, B: 1, State: mesh.PairUp}, {A: 0, B: 2, State: mesh.PairDown}, {A: 1, B: 2, State: mesh.PairUp},
	}, Findings: []mesh.Finding{{Kind: "link", Nodes: []int64{0, 2}}}}
	r := mesh.Report{Verdict: v, Names: mesh.Names{0: "Ana sunucu", 1: "İzmir", 2: "Muğla"}}
	img, err := mesh.RenderPNG(mesh.BuildTopology(v), r.Names, "tr")
	if err != nil {
		t.Fatal(err)
	}
	return Event{Kind: KindMesh, Lang: "tr", Time: time.Unix(1_700_000_000, 0), Mesh: &r, Image: img}
}

func TestMailInlineImage(t *testing.T) {
	ev := meshMailEvent(t)
	root := parseMail(t, buildMail("Bekci <a@b.c>", "x@y.z", ev))
	if root.ctype != "multipart/alternative" || len(root.children) != 2 {
		t.Fatalf("kök: %s %d parça", root.ctype, len(root.children))
	}
	plain, rel := root.children[0], root.children[1]
	if plain.ctype != "text/plain" || !strings.Contains(plain.body, "Muğla") {
		t.Fatalf("düz metin okunmalı: %s %q", plain.ctype, plain.body)
	}
	if rel.ctype != "multipart/related" || len(rel.children) != 2 {
		t.Fatalf("related: %s %d", rel.ctype, len(rel.children))
	}
	html, img := rel.children[0], rel.children[1]
	if html.ctype != "text/html" || !strings.Contains(html.body, `src="cid:mesh"`) || !strings.Contains(html.body, `alt="Bağlantı şeması: 2 sağlıklı · 1 koptu"`) {
		t.Fatalf("html görüntü etiketi: %s", html.body)
	}
	if img.ctype != "image/png" || img.cid != "<mesh>" || !strings.HasPrefix(img.disp, "inline") {
		t.Fatalf("görüntü parçası: %+v", img.ctype+" "+img.cid+" "+img.disp)
	}
	if img.body != string(ev.Image) {
		t.Fatalf("PNG base64 çözümü farklı (%d / %d bayt)", len(img.body), len(ev.Image))
	}

	// görüntü yok: bugünkü yapı (text/plain + text/html), cid yok
	ev.Image = nil
	root = parseMail(t, buildMail("Bekci <a@b.c>", "x@y.z", ev))
	if len(root.children) != 2 || root.children[0].ctype != "text/plain" || root.children[1].ctype != "text/html" || strings.Contains(root.children[1].body, "cid:") {
		t.Fatalf("görüntüsüz yapı değişti: %+v", root.children[1].ctype)
	}
}
