package check

import (
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
)

func TestGRPC(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	hs := health.NewServer()
	hs.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	hs.SetServingStatus("iyi-servis", grpc_health_v1.HealthCheckResponse_SERVING)
	hs.SetServingStatus("kotu-servis", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	grpc_health_v1.RegisterHealthServer(srv, hs)
	go srv.Serve(lis)
	defer srv.Stop()

	addr := lis.Addr().String()

	if r := run(t, "grpc", map[string]any{"target": addr}); !r.Up {
		t.Errorf("varsayılan servis: %+v", r)
	}
	if r := run(t, "grpc", map[string]any{"target": addr, "service": "iyi-servis"}); !r.Up {
		t.Errorf("iyi servis: %+v", r)
	}
	if r := run(t, "grpc", map[string]any{"target": addr, "service": "kotu-servis"}); r.Up || !strings.Contains(r.Message, "NOT_SERVING") {
		t.Errorf("kötü servis: %+v", r)
	}
	if r := run(t, "grpc", map[string]any{"target": addr, "service": "olmayan-servis"}); r.Up || !strings.Contains(r.Message, "bulunamadı") {
		t.Errorf("olmayan servis: %+v", r)
	}

	// Bağlantı reddi: geçici dinleyici kapatılınca port boş kalır.
	freeLis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	freeAddr := freeLis.Addr().String()
	freeLis.Close()
	if r := run(t, "grpc", map[string]any{"target": freeAddr}); r.Up {
		t.Errorf("bağlantı reddi bekleniyordu: %+v", r)
	}
}

func TestGRPCNormalize(t *testing.T) {
	c, _ := Get("grpc")
	bad := []string{
		`{}`,
		`{"target":"sadece-host"}`,
		`{"target":"host:port","metadata":"bozuk satır"}`,
		`{"target":"host:1","bilinmeyen":1}`,
	}
	for _, b := range bad {
		_, err := c.Normalize(json.RawMessage(b))
		var ve ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("%s: doğrulama hatası bekleniyordu, %v geldi", b, err)
		}
	}
	norm, err := c.Normalize(json.RawMessage(`{"target":" 127.0.0.1:5000 ","service":" svc "}`))
	if err != nil {
		t.Fatal(err)
	}
	var cfg GRPCConfig
	json.Unmarshal(norm, &cfg)
	if cfg.Target != "127.0.0.1:5000" || cfg.Service != "svc" {
		t.Errorf("normalize yanlış: %+v", cfg)
	}
}
