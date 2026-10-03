package webpush

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// RFC 8291 Appendix A test vektörü: aynı geçici anahtar ve salt ile birebir
// aynı şifreli çıktı üretilmeli.
func TestEncryptRFC8291Vector(t *testing.T) {
	var sub Subscription
	sub.Keys.P256dh = "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"
	sub.Keys.Auth = "BTBZMqHH6r4Tts7J_aSIgg"
	asPriv, _ := b64.DecodeString("yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw")
	eph, err := ecdh.P256().NewPrivateKey(asPriv)
	if err != nil {
		t.Fatal(err)
	}
	salt, _ := b64.DecodeString("DGv6ra1nlYgDCS1FRnbzlw")
	got, err := encryptWith(sub, []byte("When I grow up, I want to be a watermelon"), eph, salt)
	if err != nil {
		t.Fatal(err)
	}
	want := "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPTpK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN"
	if b64.EncodeToString(got) != want {
		t.Fatalf("şifreli çıktı RFC vektörüyle uyuşmuyor:\n got  %s\n want %s", b64.EncodeToString(got), want)
	}
}

// decrypt test yardımcısı: aes128gcm gövdesini istemci anahtarıyla açar.
func decrypt(t *testing.T, uaPriv *ecdh.PrivateKey, auth, body []byte) []byte {
	t.Helper()
	salt := body[:16]
	idlen := int(body[20])
	asPub := body[21 : 21+idlen]
	ct := body[21+idlen:]
	remote, err := ecdh.P256().NewPublicKey(asPub)
	if err != nil {
		t.Fatal(err)
	}
	shared, _ := uaPriv.ECDH(remote)
	info := append(append([]byte("WebPush: info\x00"), uaPriv.PublicKey().Bytes()...), asPub...)
	ikm, _ := hkdf.Key(sha256.New, shared, auth, string(info), 32)
	cek, _ := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	nonce, _ := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	block, _ := aes.NewCipher(cek)
	gcm, _ := cipher.NewGCM(block)
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		t.Fatalf("açılamadı: %v", err)
	}
	if binary.BigEndian.Uint32(body[16:20]) != recordSize || pt[len(pt)-1] != 0x02 {
		t.Fatalf("kayıt biçimi: rs=%d son=%x", binary.BigEndian.Uint32(body[16:20]), pt[len(pt)-1])
	}
	return pt[:len(pt)-1]
}

// Rastgele anahtarlarla gidiş-dönüş; VAPID başlığı doğrulanır; 410 → ErrGone.
func TestSendRoundTrip(t *testing.T) {
	keys, err := GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	ua, _ := ecdh.P256().GenerateKey(nil)
	auth := []byte("0123456789abcdef")
	var sub Subscription
	sub.Keys.P256dh = b64.EncodeToString(ua.PublicKey().Bytes())
	sub.Keys.Auth = b64.EncodeToString(auth)
	var gotPayload []byte
	status := 201
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Encoding") != "aes128gcm" || r.Header.Get("TTL") != "60" || r.Header.Get("Urgency") != "high" || r.Header.Get("Topic") != "inc-1" {
			t.Errorf("başlıklar: %v", r.Header)
		}
		// VAPID: "vapid t=<jwt>, k=<pub>"; imza açık anahtarla doğrulanır, aud bizim kökümüz.
		authz := r.Header.Get("Authorization")
		parts := strings.Split(strings.TrimPrefix(authz, "vapid "), ", ")
		if len(parts) != 2 || !strings.HasPrefix(parts[0], "t=") || parts[1] != "k="+keys.Public {
			t.Errorf("VAPID başlığı: %s", authz)
		}
		jwt := strings.Split(strings.TrimPrefix(parts[0], "t="), ".")
		pubBytes, _ := b64.DecodeString(keys.Public)
		x, y := elliptic.Unmarshal(elliptic.P256(), pubBytes) //nolint:staticcheck
		pub := &ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}
		sig, _ := b64.DecodeString(jwt[2])
		sum := sha256.Sum256([]byte(jwt[0] + "." + jwt[1]))
		if !ecdsa.Verify(pub, sum[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
			t.Error("VAPID imzası doğrulanamadı")
		}
		var claims map[string]any
		cb, _ := b64.DecodeString(jwt[1])
		json.Unmarshal(cb, &claims)
		if claims["aud"] != strings.TrimSuffix(srv0(r), "/") || claims["sub"] != "mailto:ops@ornek.com" {
			t.Errorf("claims: %v", claims)
		}
		body := make([]byte, 0, 4096)
		buf := make([]byte, 4096)
		for {
			n, err := r.Body.Read(buf)
			body = append(body, buf[:n]...)
			if err != nil {
				break
			}
		}
		gotPayload = decrypt(t, ua, auth, body)
		w.WriteHeader(status)
	}))
	defer srv.Close()
	sub.Endpoint = srv.URL + "/push/abc"
	payload := []byte(`{"title":"🔴 site çalışmıyor","url":"/#/incidents/1"}`)
	err = Send(context.Background(), srv.Client(), keys, sub, payload, Options{TTL: 60, Urgency: "high", Topic: "inc-1", Subject: "mailto:ops@ornek.com"})
	if err != nil {
		t.Fatal(err)
	}
	if string(gotPayload) != string(payload) {
		t.Fatalf("içerik: %q", gotPayload)
	}
	status = 410
	if err := Send(context.Background(), srv.Client(), keys, sub, payload, Options{TTL: 60, Urgency: "high", Topic: "inc-1", Subject: "mailto:ops@ornek.com"}); !errors.Is(err, ErrGone) {
		t.Fatalf("410 → ErrGone bekleniyordu: %v", err)
	}
	// Çok uzun içerik ve bozuk anahtar.
	if _, err := Encrypt(sub, make([]byte, 5000)); err == nil {
		t.Fatal("uzun içerik reddedilmeli")
	}
	bad := sub
	bad.Keys.Auth = "kisa"
	if _, err := Encrypt(bad, payload); err == nil {
		t.Fatal("bozuk auth reddedilmeli")
	}
	if _, err := (Keys{Private: "x", Public: keys.Public}).Authorization(sub.Endpoint, "mailto:a@b", time.Now()); err == nil {
		t.Fatal("bozuk özel anahtar reddedilmeli")
	}
}

// srv0 isteğin kökü (şema + sunucu); httptest TLS kullanmaz.
func srv0(r *http.Request) string { return "http://" + r.Host }
