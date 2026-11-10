package eklenti

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestParseRange(t *testing.T) {
	inf := math.Inf(1)
	cases := []struct {
		in      string
		want    Range
		bad     bool
		str     string
		alertAt map[float64]bool
	}{
		{in: "10", want: Range{0, 10, false}, str: "10", alertAt: map[float64]bool{-1: true, 0: false, 10: false, 10.1: true}},
		{in: "10:", want: Range{10, inf, false}, str: "10:", alertAt: map[float64]bool{9.9: true, 10: false, 1e9: false}},
		{in: "~:10", want: Range{math.Inf(-1), 10, false}, str: "~:10", alertAt: map[float64]bool{-1e9: false, 10: false, 11: true}},
		{in: "10:20", want: Range{10, 20, false}, str: "10:20", alertAt: map[float64]bool{9: true, 10: false, 20: false, 21: true}},
		{in: "@10:20", want: Range{10, 20, true}, str: "@10:20", alertAt: map[float64]bool{9: false, 10: true, 15: true, 20: true, 21: false}},
		{in: "@10", want: Range{0, 10, true}, str: "@10", alertAt: map[float64]bool{-1: false, 5: true}},
		{in: "-5:-1", want: Range{-5, -1, false}, str: "-5:-1"},
		{in: "0.5:1.5", want: Range{0.5, 1.5, false}, str: "0.5:1.5"},
		{in: ":20", want: Range{0, 20, false}, str: "20"},
		{in: " 7 ", want: Range{0, 7, false}, str: "7"},
		{in: "", bad: true},
		{in: "@", bad: true},
		{in: "20:10", bad: true},
		{in: "abc", bad: true},
		{in: "inf", bad: true},
		{in: "NaN", bad: true},
		{in: "1:2:3", bad: true},
		{in: "0x10", bad: true},
		{in: "~", bad: true},
		{in: strings.Repeat("1", 100), bad: true},
	}
	for _, c := range cases {
		r, err := ParseRange(c.in)
		if c.bad {
			if err == nil {
				t.Errorf("%q: hata bekleniyordu, %+v", c.in, r)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if r != c.want {
			t.Errorf("%q: %+v, beklenen %+v", c.in, r, c.want)
		}
		if r.String() != c.str {
			t.Errorf("%q: String %q, beklenen %q", c.in, r.String(), c.str)
		}
		for v, want := range c.alertAt {
			if r.Alert(v) != want {
				t.Errorf("%q: Alert(%v) = %v", c.in, v, !want)
			}
		}
	}
}

func metric(t *testing.T, r Result, name string) Metric {
	t.Helper()
	for _, m := range r.Metrics {
		if m.Name == name {
			return m
		}
	}
	t.Fatalf("metrik yok: %s (%+v)", name, r.Metrics)
	return Metric{}
}

func near(a, b float64) bool { return math.Abs(a-b) <= 1e-9*math.Max(1, math.Abs(b)) }

func TestNagiosStatus(t *testing.T) {
	for code, want := range map[int]Status{0: OK, 1: Warning, 2: Critical, 3: Unknown, 4: Unknown, 127: Unknown, -1: Unknown, 255: Unknown} {
		r := ParseNagios(code, []byte("TEXT"))
		if r.Status != want || r.Message != "TEXT" {
			t.Errorf("kod %d: %+v", code, r)
		}
	}
}

func TestNagiosPerfdata(t *testing.T) {
	cases := []struct {
		name  string
		out   string
		check func(t *testing.T, r Result)
	}{
		{"yalnız metin", "DISK OK - free space: 10 GB\n", func(t *testing.T, r Result) {
			if r.Message != "DISK OK - free space: 10 GB" || len(r.Metrics) != 0 || r.Dropped != 0 {
				t.Fatalf("%+v", r)
			}
		}},
		{"birim çevirme", "OK | time=250ms;500;1000;0 size=2KB;;;0;4 used=95%;80;90;0;100 req=12c up=3.5s tiny=40us", func(t *testing.T, r Result) {
			m := metric(t, r, "time")
			if !near(m.Value, 0.25) || m.Unit != "s" || !near(m.Warn.End, 0.5) || !near(m.Crit.End, 1) || *m.Min != 0 || m.Max != nil {
				t.Fatalf("time %+v w=%+v", m, m.Warn)
			}
			m = metric(t, r, "size")
			if m.Value != 2048 || m.Unit != "B" || m.Warn != nil || *m.Max != 4096 {
				t.Fatalf("size %+v", m)
			}
			m = metric(t, r, "used")
			if m.Value != 95 || m.Unit != "%" || !m.Warn.Alert(95) || !m.Crit.Alert(95) || *m.Max != 100 {
				t.Fatalf("used %+v", m)
			}
			if metric(t, r, "req").Unit != "c" || metric(t, r, "up").Value != 3.5 || !near(metric(t, r, "tiny").Value, 40e-6) {
				t.Fatalf("%+v", r.Metrics)
			}
			if r.Message != "OK" {
				t.Fatalf("ileti %q", r.Message)
			}
		}},
		{"MB GB TB", "x | a=1MB b=1GB c=1TB d=1.5kB", func(t *testing.T, r Result) {
			if metric(t, r, "a").Value != 1<<20 || metric(t, r, "b").Value != 1<<30 || metric(t, r, "c").Value != 1<<40 || metric(t, r, "d").Value != 1536 {
				t.Fatalf("%+v", r.Metrics)
			}
		}},
		{"tırnaklı etiket", "OK | 'time used'=1s;; '/var'=10% 'it''s'=3 '/'=1", func(t *testing.T, r Result) {
			metric(t, r, "time_used")
			metric(t, r, "_var")
			metric(t, r, "it_s")
			metric(t, r, "_")
		}},
		{"aralıklar", "OK | a=5;10:;~:3;0 b=15;@10:20;20: c=1;20:10;abc", func(t *testing.T, r Result) {
			a := metric(t, r, "a")
			if a.Warn.String() != "10:" || a.Crit.String() != "~:3" {
				t.Fatalf("a %v %v", a.Warn, a.Crit)
			}
			b := metric(t, r, "b")
			if !b.Warn.Inside || !b.Warn.Alert(15) || !b.Crit.Alert(15) {
				t.Fatalf("b %+v", b)
			}
			c := metric(t, r, "c")
			if c.Warn != nil || c.Crit != nil {
				t.Fatalf("geçersiz eşik atılmalıydı: %+v", c)
			}
		}},
		{"ms eşik çevirme", "OK | rt=50ms;@100:200", func(t *testing.T, r Result) {
			m := metric(t, r, "rt")
			if m.Warn.String() != "@0.1:0.2" {
				t.Fatalf("%v", m.Warn)
			}
		}},
		{"çok satır", "HTTP OK | a=1\nuzun satır 1\nuzun satır 2 | b=2s\nc=3;4;5\n'd e'=4ms\n", func(t *testing.T, r Result) {
			if r.Message != "HTTP OK" || len(r.Metrics) != 4 {
				t.Fatalf("%+v", r)
			}
			if !near(metric(t, r, "d_e").Value, 0.004) || metric(t, r, "c").Crit.End != 5 {
				t.Fatalf("%+v", r.Metrics)
			}
		}},
		{"ilk satırda perfdata yok", "OK\nayrıntı\nayrıntı2 | x=1\ny=2", func(t *testing.T, r Result) {
			if len(r.Metrics) != 2 || r.Message != "OK" {
				t.Fatalf("%+v", r)
			}
		}},
		{"CRLF", "OK | a=1\r\nuzun | b=2\r\n", func(t *testing.T, r Result) {
			if len(r.Metrics) != 2 || metric(t, r, "b").Value != 2 {
				t.Fatalf("%+v", r)
			}
		}},
		{"bozuk öğeler atılır", "OK | a=U b= =3 c d=1x2 'e=1 f=NaN g=Inf h=1e999 i=-2.5e3 j=0,5 k=.5 l=7", func(t *testing.T, r Result) {
			names := []string{}
			for _, m := range r.Metrics {
				names = append(names, m.Name)
			}
			got := strings.Join(names, ",")
			if got != "d,i,j,k,l" {
				t.Fatalf("metrikler %s (dropped %d)", got, r.Dropped)
			}
			if metric(t, r, "i").Value != -2500 || metric(t, r, "j").Value != 0.5 || metric(t, r, "k").Value != 0.5 || metric(t, r, "d").Unit != "" {
				t.Fatalf("%+v", r.Metrics)
			}
			if r.Dropped < 7 {
				t.Fatalf("dropped %d", r.Dropped)
			}
		}},
		{"yinelenen ad", "OK | a=1 a=2", func(t *testing.T, r Result) {
			if len(r.Metrics) != 1 || r.Metrics[0].Value != 1 || r.Dropped != 1 {
				t.Fatalf("%+v", r)
			}
		}},
		{"tanınmayan birim", "OK | a=3rpm b=4;;;; c=5m/s", func(t *testing.T, r Result) {
			if metric(t, r, "a").Unit != "rpm" || metric(t, r, "c").Unit != "m/s" {
				t.Fatalf("%+v", r.Metrics)
			}
		}},
		{"uzun ad kesilir", "OK | " + strings.Repeat("a", 100) + "=1", func(t *testing.T, r Result) {
			if len(r.Metrics[0].Name) != 64 {
				t.Fatalf("%d", len(r.Metrics[0].Name))
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { c.check(t, ParseNagios(0, []byte(c.out))) })
	}
}

func TestNagiosLimits(t *testing.T) {
	var b strings.Builder
	b.WriteString("OK |")
	for i := 0; i < 60; i++ {
		b.WriteString(" m")
		b.WriteString(strings.Repeat("x", i%3))
		b.WriteString(string(rune('a'+i%26)) + string(rune('a'+i/26)))
		b.WriteString("=1")
	}
	r := ParseNagios(0, []byte(b.String()))
	if len(r.Metrics) != MaxMetrics || r.Dropped != 10 {
		t.Fatalf("metrik %d dropped %d", len(r.Metrics), r.Dropped)
	}

	long := strings.Repeat("ç", 600) + "\x00\x1b[31m"
	r = ParseNagios(0, []byte(long))
	if utf8.RuneCountInString(r.Message) != MaxMessage || strings.ContainsAny(r.Message, "\x00\x1b") {
		t.Fatalf("ileti %d", utf8.RuneCountInString(r.Message))
	}

	big := "OK | a=1\n" + strings.Repeat("satır\n", MaxOutput/6) + "| b=2\n"
	r = ParseNagios(0, []byte(big))
	if !r.Truncated || len(r.Metrics) != 1 {
		t.Fatalf("%+v", r.Metrics)
	}

	r = ParseNagios(0, []byte("OK \xff\xfe bozuk | a=1"))
	if !utf8.ValidString(r.Message) {
		t.Fatalf("geçersiz UTF-8: %q", r.Message)
	}
}

func TestParseJSONOurs(t *testing.T) {
	in := `{"status":"warning","message":"yavaş\nyanıt","metrics":{
		"rt":{"value":250,"unit":"ms","warn":"200","crit":"@500:"},
		"conn":{"value":12,"warn":10,"min":0,"max":100},
		"plain":3.5,
		"bad name":1,
		"noval":{"unit":"s"},
		"str":{"value":"12"},
		"disk":{"value":1,"unit":"GB","crit":"20:10"}}}`
	r, err := ParseJSON([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != Warning || r.Message != "yavaş yanıt" {
		t.Fatalf("%+v", r)
	}
	rt := metric(t, r, "rt")
	if !near(rt.Value, 0.25) || rt.Unit != "s" || !near(rt.Warn.End, 0.2) || !rt.Crit.Inside || !near(rt.Crit.Start, 0.5) {
		t.Fatalf("rt %+v", rt)
	}
	conn := metric(t, r, "conn")
	if conn.Warn.String() != "10" || *conn.Max != 100 {
		t.Fatalf("conn %+v", conn)
	}
	if metric(t, r, "plain").Value != 3.5 {
		t.Fatal("plain")
	}
	d := metric(t, r, "disk")
	if d.Value != 1<<30 || d.Crit != nil {
		t.Fatalf("disk %+v", d)
	}
	if len(r.Metrics) != 4 || r.Dropped != 3 {
		t.Fatalf("metrik %d dropped %d", len(r.Metrics), r.Dropped)
	}
	// sıralı
	if r.Metrics[0].Name != "conn" || r.Metrics[3].Name != "rt" {
		t.Fatalf("sıra %+v", r.Metrics)
	}
}

func TestParseJSONFlat(t *testing.T) {
	cases := []struct {
		in     string
		status Status
		msg    string
		n      int
	}{
		{`{"queue":12,"latency":40,"units":{"latency":"ms"},"status":1,"msg":"tamam","version":"1.2","nested":{"a":1}}`, OK, "tamam", 2},
		{`{"queue":12,"status":0,"msg":"kuyruk dolu"}`, Critical, "kuyruk dolu", 1},
		{`{"queue":12}`, OK, "", 1},
		{`{"a":1,"status":"warning"}`, Warning, "", 1},
		{`{"a":1,"status":true}`, Unknown, "", 1},
		{`{"a":1,"message":"m"}`, OK, "m", 1},
	}
	for _, c := range cases {
		r, err := ParseJSON([]byte(c.in))
		if err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		if r.Status != c.status || r.Message != c.msg || len(r.Metrics) != c.n {
			t.Errorf("%s: %+v", c.in, r)
		}
	}
	r, _ := ParseJSON([]byte(cases[0].in))
	if l := metric(t, r, "latency"); !near(l.Value, 0.04) || l.Unit != "s" {
		t.Fatalf("%+v", l)
	}
}

func TestParseJSONErrors(t *testing.T) {
	for _, in := range []string{``, `[]`, `null`, `{`, `"x"`, `12`} {
		if _, err := ParseJSON([]byte(in)); err == nil {
			t.Errorf("%q: hata bekleniyordu", in)
		}
	}
	big := `{"a":"` + strings.Repeat("x", MaxOutput) + `"}`
	r, err := ParseJSON([]byte(big))
	if err == nil || !r.Truncated {
		t.Fatal("büyük çıktı reddedilmeli")
	}
	// Parse: hata unknown olur, zaman aşımı her zaman unknown.
	if r := Parse("json", 0, []byte("{bozuk")); r.Status != Unknown || !strings.Contains(r.Message, "invalid JSON") {
		t.Fatalf("%+v", r)
	}
	if r := Parse("json", -1, []byte(`{"status":"ok","metrics":{}}`)); r.Status != Unknown {
		t.Fatalf("%+v", r)
	}
	if r := Parse("nagios", 1, []byte("W | a=1")); r.Status != Warning || len(r.Metrics) != 1 {
		t.Fatalf("%+v", r)
	}
}

func TestJSONLimit(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"status":"ok","metrics":{`)
	for i := 0; i < 70; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`"m` + string(rune('a'+i/26)) + string(rune('a'+i%26)) + `":1`)
	}
	b.WriteString(`}}`)
	r, err := ParseJSON([]byte(b.String()))
	if err != nil || len(r.Metrics) != MaxMetrics || r.Dropped != 20 {
		t.Fatalf("%v %d %d", err, len(r.Metrics), r.Dropped)
	}
}

func TestResultJSON(t *testing.T) {
	r := ParseNagios(1, []byte("W | rt=150ms;@100:200;~:300"))
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"status":"warning","message":"W","metrics":[{"name":"rt","value":0.15,"unit":"s","warn":"@0.1:0.2","crit":"~:0.3"}]}`
	if string(b) != want {
		t.Fatalf("%s", b)
	}
	var back Result
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if !back.Metrics[0].Warn.Alert(0.15) || back.Metrics[0].Crit.Alert(0.15) {
		t.Fatalf("%+v", back.Metrics[0])
	}
}

// checkInvariants, her sonucun sınırlara uyduğunu denetler.
func checkInvariants(t *testing.T, r Result) {
	if !r.Status.Valid() {
		t.Fatalf("durum %q", r.Status)
	}
	if utf8.RuneCountInString(r.Message) > MaxMessage || !utf8.ValidString(r.Message) || strings.ContainsAny(r.Message, "\n\r\x00") {
		t.Fatalf("ileti %q", r.Message)
	}
	if r.Metrics == nil || len(r.Metrics) > MaxMetrics {
		t.Fatalf("metrik sayısı %d", len(r.Metrics))
	}
	seen := map[string]bool{}
	for _, m := range r.Metrics {
		if !ValidName(m.Name) || seen[m.Name] || !finite(m.Value) {
			t.Fatalf("metrik %+v", m)
		}
		seen[m.Name] = true
		for _, rg := range []*Range{m.Warn, m.Crit} {
			if rg != nil && (math.IsNaN(rg.Start) || math.IsNaN(rg.End) || rg.Start > rg.End) {
				t.Fatalf("aralık %+v", rg)
			}
		}
		if (m.Min != nil && !finite(*m.Min)) || (m.Max != nil && !finite(*m.Max)) {
			t.Fatalf("min/max %+v", m)
		}
	}
	if _, err := json.Marshal(r); err != nil {
		t.Fatalf("json: %v", err)
	}
}

func FuzzNagios(f *testing.F) {
	for _, s := range []string{
		"OK | a=1", "W | 'x y'=1ms;10:;~:3;0;100", "C\nl1 | b=2\nc=3", "| =", "'", "a=1;@;~:;:", "OK | x=1e308TB;1e308",
	} {
		f.Add(0, []byte(s))
	}
	f.Fuzz(func(t *testing.T, code int, out []byte) {
		checkInvariants(t, ParseNagios(code, out))
	})
}

func FuzzJSON(f *testing.F) {
	for _, s := range []string{
		`{"status":"ok","metrics":{"a":{"value":1,"unit":"ms","warn":"@1:2"}}}`,
		`{"a":1,"units":{"a":"GB"},"status":0,"msg":"x"}`, `{"metrics":{"a":1e308,"b":{"value":1e308,"unit":"TB","max":1e308}}}`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, out []byte) {
		r, _ := ParseJSON(out)
		checkInvariants(t, r)
		checkInvariants(t, Parse("json", 0, out))
	})
}

func FuzzRange(f *testing.F) {
	for _, s := range []string{"10", "10:", "~:10", "10:20", "@10:20", "@~:", ":"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		r, err := ParseRange(s)
		if err != nil {
			return
		}
		if math.IsNaN(r.Start) || math.IsNaN(r.End) || r.Start > r.End {
			t.Fatalf("%q -> %+v", s, r)
		}
		back, err := ParseRange(r.String())
		if err != nil || back != r {
			t.Fatalf("%q -> %q -> %+v %v", s, r.String(), back, err)
		}
	})
}
