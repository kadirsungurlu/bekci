//go:build windows

package metrics

import (
	"context"
	"encoding/hex"
	"sort"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// windowsCerts LocalMachine My + WebHosting depolarını, IIS (http.sys) SSL
// bağlantılarını ve MSSQL sertifika parmak izlerini okur. Hata veren kaynak
// atlanır; depo okunamazsa liste boş kalır (envanterin diğer bölümleri etkilenmez).
func windowsCerts(ctx context.Context) []InvCert {
	var certs []winStoreCert
	for _, st := range []string{"My", "WebHosting"} {
		certs = append(certs, readCertStore(st)...)
		if ctx.Err() != nil {
			return nil
		}
	}
	binds := append(httpSSLBindings(), mssqlBindings()...)
	return winStoreInv(certs, binds)
}

// readCertStore depoyu salt okunur açar; depo yoksa (WebHosting IIS'siz
// sunucuda olmayabilir) boş döner.
func readCertStore(name string) []winStoreCert {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil
	}
	h, err := windows.CertOpenStore(windows.CERT_STORE_PROV_SYSTEM_W, 0, 0,
		windows.CERT_SYSTEM_STORE_LOCAL_MACHINE|windows.CERT_STORE_READONLY_FLAG|windows.CERT_STORE_OPEN_EXISTING_FLAG,
		uintptr(unsafe.Pointer(p)))
	if err != nil {
		return nil
	}
	defer windows.CertCloseStore(h, 0)
	var out []winStoreCert
	var cc *windows.CertContext
	for len(out) < MaxInventoryCerts*2 {
		cc, err = windows.CertEnumCertificatesInStore(h, cc)
		if err != nil || cc == nil {
			break
		}
		der := make([]byte, cc.Length)
		copy(der, unsafe.Slice(cc.EncodedCert, cc.Length))
		out = append(out, winStoreCert{Store: name, DER: der})
	}
	if cc != nil {
		windows.CertFreeCertificateContext(cc)
	}
	return out
}

// http.sys SSL yapılandırması (IIS bağlantıları httpapi'de tutulur).
var (
	modhttpapi                 = windows.NewLazySystemDLL("httpapi.dll")
	procHttpInitialize         = modhttpapi.NewProc("HttpInitialize")
	procHttpTerminate          = modhttpapi.NewProc("HttpTerminate")
	procHttpQueryServiceConfig = modhttpapi.NewProc("HttpQueryServiceConfiguration")
)

const (
	httpInitializeConfig     = 2
	httpConfigSSLCertInfo    = 1 // HttpServiceConfigSSLCertInfo (ip:port)
	httpConfigSSLSniCertInfo = 5 // HttpServiceConfigSslSniCertInfo (ad:port)
	httpQueryNext            = 1
	maxHTTPBindings          = 1000
)

type httpSSLParam struct {
	HashLen    uint32
	Hash       *byte
	AppID      windows.GUID
	StoreName  *uint16
	CheckMode  uint32
	RevFresh   uint32
	RevTimeout uint32
	CtlID      *uint16
	CtlStore   *uint16
	Flags      uint32
}

// HTTP_SERVICE_CONFIG_SSL_QUERY / _SET (anahtar SOCKADDR göstericisi).
type httpSSLQuery struct {
	QueryDesc uint32
	IPPort    *byte
	Token     uint32
}
type httpSSLSet struct {
	IPPort *byte
	Param  httpSSLParam
}

// HTTP_SERVICE_CONFIG_SSL_SNI_QUERY / _SET (SOCKADDR_STORAGE 128 bayt, 8 hizalı).
type httpSNIQuery struct {
	QueryDesc uint32
	IPPort    [16]uint64
	Host      *uint16
	Token     uint32
}
type httpSNISet struct {
	IPPort [16]uint64
	Host   *uint16
	Param  httpSSLParam
}

func httpSSLBindings() []winBinding {
	if modhttpapi.Load() != nil {
		return nil
	}
	ver := uint32(1) // HTTPAPI_VERSION{1, 0}
	if r, _, _ := procHttpInitialize.Call(uintptr(ver), httpInitializeConfig, 0); r != 0 {
		return nil
	}
	defer procHttpTerminate.Call(httpInitializeConfig, 0)
	var out []winBinding
	buf := make([]uint64, 512)
	query := func(id uintptr, q unsafe.Pointer, qlen uintptr) (unsafe.Pointer, bool) {
		for range 3 {
			var ret uint32
			r, _, _ := procHttpQueryServiceConfig.Call(0, id, uintptr(q), qlen,
				uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)*8), uintptr(unsafe.Pointer(&ret)), 0)
			switch windows.Errno(r) {
			case 0:
				return unsafe.Pointer(&buf[0]), true
			case windows.ERROR_INSUFFICIENT_BUFFER:
				buf = make([]uint64, ret/8+1)
				continue
			}
			return nil, false
		}
		return nil, false
	}
	for i := uint32(0); i < maxHTTPBindings; i++ {
		q := httpSSLQuery{QueryDesc: httpQueryNext, Token: i}
		p, ok := query(httpConfigSSLCertInfo, unsafe.Pointer(&q), unsafe.Sizeof(q))
		if !ok {
			break
		}
		s := (*httpSSLSet)(p)
		if s.IPPort == nil || s.Param.Hash == nil {
			continue
		}
		addr := sockaddrString(sockaddrBytes(s.IPPort))
		out = append(out, winBinding{Thumb: hex.EncodeToString(unsafe.Slice(s.Param.Hash, s.Param.HashLen)), Use: "IIS " + addr})
	}
	for i := uint32(0); i < maxHTTPBindings; i++ {
		q := httpSNIQuery{QueryDesc: httpQueryNext, Token: i}
		p, ok := query(httpConfigSSLSniCertInfo, unsafe.Pointer(&q), unsafe.Sizeof(q))
		if !ok {
			break
		}
		s := (*httpSNISet)(p)
		if s.Param.Hash == nil {
			continue
		}
		raw := unsafe.Slice((*byte)(unsafe.Pointer(&s.IPPort[0])), 28)
		port := sockaddrString(raw)
		if k := lastColon(port); k >= 0 {
			port = port[k:]
		}
		host := windows.UTF16PtrToString(s.Host)
		out = append(out, winBinding{Thumb: hex.EncodeToString(unsafe.Slice(s.Param.Hash, s.Param.HashLen)), Use: "IIS " + host + port})
	}
	return out
}

// sockaddrBytes aileye göre SOCKADDR_IN (16) ya da SOCKADDR_IN6 (28) baytı.
func sockaddrBytes(p *byte) []byte {
	if *(*uint16)(unsafe.Pointer(p)) == 23 {
		return unsafe.Slice(p, 28)
	}
	return unsafe.Slice(p, 16)
}

func lastColon(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == ':' {
			return i
		}
	}
	return -1
}

// mssqlBindings her SQL Server örneğinin SuperSocketNetLib\Certificate
// değerini okur (örnek adı -> örnek kimliği "Instance Names\SQL" altında).
func mssqlBindings() []winBinding {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Microsoft SQL Server\Instance Names\SQL`, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return nil
	}
	defer k.Close()
	names, err := k.ReadValueNames(0)
	if err != nil {
		return nil
	}
	sort.Strings(names)
	var out []winBinding
	for _, n := range names {
		id, _, err := k.GetStringValue(n)
		if err != nil || id == "" {
			continue
		}
		nk, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Microsoft SQL Server\`+id+`\MSSQLServer\SuperSocketNetLib`, registry.QUERY_VALUE|registry.WOW64_64KEY)
		if err != nil {
			continue
		}
		thumb, _, err := nk.GetStringValue("Certificate")
		nk.Close()
		if err != nil || normThumb(thumb) == "" {
			continue
		}
		out = append(out, winBinding{Thumb: thumb, Use: "MSSQL " + n})
	}
	return out
}
