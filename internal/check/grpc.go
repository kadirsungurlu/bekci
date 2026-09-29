package check

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// GRPCConfig gRPC sağlık kontrolü (grpc.health.v1.Health/Check) ayarı.
type GRPCConfig struct {
	Target    string `json:"target"` // host:port
	Service   string `json:"service"`
	TLS       bool   `json:"tls"`
	IgnoreTLS bool   `json:"ignore_tls"`
	Metadata  string `json:"metadata"` // "Ad: değer" satırları
}

type grpcChecker struct{}

func init() { Register("grpc", grpcChecker{}) }

func (grpcChecker) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c GRPCConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	c.Target = strings.TrimSpace(c.Target)
	if c.Target == "" {
		return nil, invalid("Sunucu adresi (host:port) gerekli")
	}
	if _, _, err := net.SplitHostPort(c.Target); err != nil {
		return nil, invalid("Geçerli bir host:port girin")
	}
	c.Service = strings.TrimSpace(c.Service)
	if _, err := parseHeaders(c.Metadata); err != nil {
		return nil, err
	}
	if !c.TLS {
		c.IgnoreTLS = false
	}
	return encode(c), nil
}

func (grpcChecker) Target(raw json.RawMessage) string {
	var c GRPCConfig
	json.Unmarshal(raw, &c)
	return c.Target
}

func (grpcChecker) CertExpiryEnabled(json.RawMessage) bool { return true }

func (grpcChecker) Check(ctx context.Context, raw json.RawMessage) (res Result) {
	var c GRPCConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return down("Ayar okunamadı: " + err.Error())
	}
	dg := newDiag("grpc", raw, c.Target)
	if host, port := hostPortOf(strings.TrimPrefix(c.Target, "dns:///"), 0); host != "" && port > 0 {
		dg.network(host, port, true)
	}
	defer dg.attach(ctx, &res)

	var creds credentials.TransportCredentials
	var tlsCfg *tls.Config
	if c.TLS {
		tlsCfg = &tls.Config{InsecureSkipVerify: c.IgnoreTLS}
		creds = credentials.NewTLS(tlsCfg)
	} else {
		creds = insecure.NewCredentials()
	}

	conn, err := grpc.NewClient(c.Target, grpc.WithTransportCredentials(creds))
	if err != nil {
		dg.fail(ctx, PhaseConfig, err)
		return down("Bağlantı oluşturulamadı: " + err.Error())
	}
	defer conn.Close()

	rctx := ctx
	if hdrs, err := parseHeaders(c.Metadata); err == nil && len(hdrs) > 0 {
		kv := make([]string, 0, len(hdrs)*2)
		for _, h := range hdrs {
			kv = append(kv, h[0], h[1])
		}
		rctx = metadata.AppendToOutgoingContext(ctx, kv...)
	}

	start := time.Now()
	client := grpc_health_v1.NewHealthClient(conn)
	resp, err := client.Check(rctx, &grpc_health_v1.HealthCheckRequest{Service: c.Service})
	ping := msSince(start)
	if err != nil {
		dg.failGRPC(ctx, err)
		return down(describeGRPCErr(ctx, err))
	}

	var cert *CertInfo
	if c.TLS {
		if info, err := dialTLSCert(ctx, "tcp", c.Target, tlsCfg); err == nil {
			cert = info
		}
	}

	if resp.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
		dg.failClass(PhaseResponse, ClassUnhealthy)
		return Result{PingMs: ping, Message: "Servis durumu: " + resp.GetStatus().String(), Cert: cert}
	}
	return Result{Up: true, PingMs: ping, Message: "SERVING" + serviceSuffix(c.Service), Cert: cert}
}

func serviceSuffix(service string) string {
	if service == "" {
		return ""
	}
	return " (" + service + ")"
}

// failGRPC sağlık çağrısının hatasını tanıya yazar. gRPC bağlantıyı çağrıyla
// birlikte kurar: Unavailable bağlanma, diğerleri çağrı aşamasıdır.
func (d *diagRun) failGRPC(ctx context.Context, err error) {
	st, ok := status.FromError(err)
	if !ok {
		d.failConn(ctx, err)
		return
	}
	switch st.Code() {
	case codes.Unavailable:
		d.failConn(ctx, err)
	case codes.DeadlineExceeded:
		d.phase, d.class, d.err = PhaseRPC, ClassTimeout, err
	case codes.NotFound, codes.Unimplemented:
		d.phase, d.class, d.err = PhaseRPC, ClassNotFound, err
	case codes.PermissionDenied, codes.Unauthenticated:
		d.phase, d.class, d.err = PhaseAuth, ClassAuth, err
	default:
		d.phase, d.class, d.err = PhaseRPC, ClassProtocol, err
	}
}

// describeGRPCErr gRPC durum kodlarını Türkçe mesajlara çevirir.
func describeGRPCErr(ctx context.Context, err error) string {
	st, ok := status.FromError(err)
	if !ok {
		return describeErr(ctx, err)
	}
	switch st.Code() {
	case codes.Unavailable:
		return "Sunucuya bağlanılamadı"
	case codes.DeadlineExceeded:
		return "Zaman aşımı"
	case codes.NotFound:
		return "Servis bulunamadı (health check tanımlı değil)"
	case codes.Unimplemented:
		return "Sunucu sağlık kontrolünü desteklemiyor (grpc.health.v1.Health)"
	case codes.PermissionDenied, codes.Unauthenticated:
		return "Yetkilendirme hatası: " + st.Message()
	default:
		return "gRPC hatası (" + st.Code().String() + "): " + st.Message()
	}
}
