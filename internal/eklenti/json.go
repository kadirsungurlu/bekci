package eklenti

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
)

// ParseJSON, iki JSON biçiminden birini ayrıştırır:
//
// Bizim şemamız (üst düzeyde "metrics" nesnesi var):
//
//	{"status":"ok|warning|critical|unknown","message":"...",
//	 "metrics":{"ad":{"value":1.5,"unit":"ms","warn":"10","crit":"@0:5"}}}
//
// Metrik değeri doğrudan sayı da olabilir ("ad": 1.5). Status eksik ya da
// tanınmıyorsa unknown.
//
// Düz biçim ("metrics" yok): üst düzey sayısal anahtarlar metriktir,
// "units" nesnesi birimleri verir, "status" 1 = ok / 0 = critical (metin
// durum adları da kabul edilir; eksikse ok), ileti "msg" (ya da "message").
//
// Metrikler ada göre sıralanır; MaxMetrics aşılırsa sıradaki fazlası atılır.
func ParseJSON(out []byte) (Result, error) {
	res := Result{Status: Unknown, Metrics: []Metric{}}
	if len(out) > MaxOutput {
		res.Truncated = true
		return res, errors.New("output exceeds 64 KiB")
	}
	var top map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(out))
	if err := dec.Decode(&top); err != nil {
		return res, err
	}
	if top == nil {
		return res, errors.New("top-level value is not an object")
	}
	if raw, ok := top["metrics"]; ok && isObject(raw) {
		return parseOurs(top, raw, res), nil
	}
	return parseFlat(top, res), nil
}

func isObject(b json.RawMessage) bool {
	b = bytes.TrimSpace(b)
	return len(b) > 0 && b[0] == '{'
}

func str(b json.RawMessage) (string, bool) {
	var s string
	if json.Unmarshal(b, &s) != nil {
		return "", false
	}
	return s, true
}

// jnum, JSON sayısını (ya da sayısal metni) sonlu float olarak çözer.
func jnum(b json.RawMessage) (float64, bool) {
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return 0, false
	}
	var n json.Number
	if b[0] == '"' {
		return 0, false
	}
	if json.Unmarshal(b, &n) != nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(string(n), 64)
	if err != nil || !finite(v) {
		return 0, false
	}
	return v, true
}

// jrange, eşik alanını (metin ya da sayı) çözer.
func jrange(b json.RawMessage, f float64) *Range {
	if len(b) == 0 {
		return nil
	}
	if s, ok := str(b); ok {
		return rangePtr(s, f)
	}
	if _, ok := jnum(b); ok {
		return rangePtr(string(bytes.TrimSpace(b)), f)
	}
	return nil
}

func statusName(s string) Status {
	st := Status(strings.ToLower(strings.TrimSpace(s)))
	if st == "warn" {
		st = Warning
	}
	if st == "crit" {
		st = Critical
	}
	if st.Valid() {
		return st
	}
	return Unknown
}

func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func parseOurs(top map[string]json.RawMessage, raw json.RawMessage, res Result) Result {
	if s, ok := str(top["status"]); ok {
		res.Status = statusName(s)
	}
	if s, ok := str(top["message"]); ok {
		res.Message = cleanMessage(s)
	}
	var ms map[string]json.RawMessage
	if json.Unmarshal(raw, &ms) != nil {
		return res
	}
	c := newCollector(&res)
	for _, name := range sortedKeys(ms) {
		item := ms[name]
		if v, ok := jnum(item); ok {
			c.add(Metric{Name: name, Value: v})
			continue
		}
		var obj map[string]json.RawMessage
		if !isObject(item) || json.Unmarshal(item, &obj) != nil {
			res.Dropped++
			continue
		}
		v, ok := jnum(obj["value"])
		if !ok {
			res.Dropped++
			continue
		}
		u := ""
		if s, ok := str(obj["unit"]); ok {
			u = s
		}
		unit, f := unitFactor(u)
		m := Metric{Name: name, Value: v * f, Unit: unit,
			Warn: jrange(obj["warn"], f), Crit: jrange(obj["crit"], f)}
		if x, ok := jnum(obj["min"]); ok && finite(x*f) {
			x *= f
			m.Min = &x
		}
		if x, ok := jnum(obj["max"]); ok && finite(x*f) {
			x *= f
			m.Max = &x
		}
		c.add(m)
	}
	return res
}

func parseFlat(top map[string]json.RawMessage, res Result) Result {
	res.Status = OK
	if b, ok := top["status"]; ok {
		if v, ok := jnum(b); ok {
			if v == 1 {
				res.Status = OK
			} else {
				res.Status = Critical
			}
		} else if s, ok := str(b); ok {
			res.Status = statusName(s)
		} else {
			res.Status = Unknown
		}
	}
	for _, k := range []string{"msg", "message"} {
		if s, ok := str(top[k]); ok {
			res.Message = cleanMessage(s)
			break
		}
	}
	units := map[string]string{}
	if b, ok := top["units"]; ok && isObject(b) {
		var um map[string]json.RawMessage
		if json.Unmarshal(b, &um) == nil {
			for k, v := range um {
				if s, ok := str(v); ok {
					units[k] = s
				}
			}
		}
	}
	c := newCollector(&res)
	for _, name := range sortedKeys(top) {
		switch name {
		case "status", "msg", "message", "units":
			continue
		}
		v, ok := jnum(top[name])
		if !ok {
			continue // sayısal olmayan anahtarlar (sürüm metni, nesne...) metrik değildir
		}
		unit, f := unitFactor(units[name])
		c.add(Metric{Name: name, Value: v * f, Unit: unit})
	}
	return res
}
