package api

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/kadirsungurlu/bekci/docs"
	"gopkg.in/yaml.v3"
)

// internalRoutes belgede yer almayan, bilerek "iç" sayılan /api/ rotaları:
// ajan protokolü (kontrol noktası ve sunucu ajanı), arayüzün kendi yardımcıları.
var internalRoutes = map[string]string{
	"GET /api/probe/jobs":                 "kontrol noktası ajan protokolü",
	"POST /api/probe/results":             "kontrol noktası ajan protokolü",
	"POST /api/probe/metrics":             "sunucu ajanı protokolü",
	"GET /api/probe/binary":               "ajan programı indirme (panel)",
	"GET /api/public/resolve":             "arayüz: özel alan adı çözümü",
	"GET /api/status-pages/{id}/preview":  "arayüz: tam ekran önizleme verisi",
	"POST /api/status-pages/preview-data": "arayüz: canlı önizleme verisi",
}

var routeRe = regexp.MustCompile(`^(GET|POST|PUT|DELETE|PATCH) (/api/\S*)$`)

// sourceRoutes internal/api/*.go (test dışı) dosyalarındaki mux.Handle /
// mux.HandleFunc çağrılarından "METHOD /api/..." kalıplarını toplar.
func sourceRoutes(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(fset, f, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (sel.Sel.Name != "Handle" && sel.Sel.Name != "HandleFunc") {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			pat, err := strconv.Unquote(lit.Value)
			if err == nil && routeRe.MatchString(pat) {
				seen[pat] = true
			}
			return true
		})
	}
	var out []string
	for r := range seen {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

// Belgedeki her yol/metot ile kaynaktaki her rota birbirini karşılamalı; iç
// rotalar açıkça listelenir. Belge geçerli YAML olmalı ve JSON'a çevrilebilmeli.
func TestOpenAPICoversRoutes(t *testing.T) {
	var spec struct {
		OpenAPI string                    `yaml:"openapi"`
		Paths   map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(docs.OpenAPI, &spec); err != nil {
		t.Fatalf("openapi.yaml: %v", err)
	}
	if !strings.HasPrefix(spec.OpenAPI, "3.") || len(spec.Paths) == 0 {
		t.Fatalf("beklenmeyen belge: sürüm %q, %d yol", spec.OpenAPI, len(spec.Paths))
	}
	documented := map[string]bool{}
	for path, ops := range spec.Paths {
		for m := range ops {
			switch m {
			case "get", "post", "put", "delete", "patch":
				documented[strings.ToUpper(m)+" "+path] = true
			}
		}
	}
	for _, r := range sourceRoutes(t) {
		if _, internal := internalRoutes[r]; internal {
			if documented[r] {
				t.Errorf("%s hem iç listede hem belgede", r)
			}
			continue
		}
		if !documented[r] {
			t.Errorf("belgede yok: %s (docs/openapi.yaml'a ekleyin ya da internalRoutes'a gerekçesiyle yazın)", r)
		}
	}
	src := map[string]bool{}
	for _, r := range sourceRoutes(t) {
		src[r] = true
	}
	for d := range documented {
		if strings.HasPrefix(d, "GET /api/") || strings.HasPrefix(d, "POST /api/") || strings.HasPrefix(d, "PUT /api/") || strings.HasPrefix(d, "DELETE /api/") {
			if !src[d] {
				t.Errorf("belgede var ama kayıtlı rota yok: %s", d)
			}
		}
	}
	for r := range internalRoutes {
		if !src[r] {
			t.Errorf("iç listede var ama kaynakta yok: %s", r)
		}
	}
	// JSON'a çevrilebilir ve sunulur.
	b, err := yamlToJSON(docs.OpenAPI)
	if err != nil {
		t.Fatal(err)
	}
	var js map[string]any
	if err := json.Unmarshal(b, &js); err != nil || js["openapi"] != spec.OpenAPI {
		t.Fatalf("JSON dönüşümü: %v", err)
	}
	e := newEnv(t)
	code, data, h := e.send("GET", "/api/openapi.json", "", nil)
	if code != http.StatusOK || !strings.HasPrefix(h.Get("Content-Type"), "application/json") || !strings.Contains(string(data), `"/api/monitors"`) {
		t.Fatalf("GET /api/openapi.json: %d %s", code, h.Get("Content-Type"))
	}
}
