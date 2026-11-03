package notify

import (
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type tgCall struct {
	path, caption, text string
	photo               []byte
}

// fakeTelegram sahte Bot API: sendPhoto (multipart) ve sendMessage (JSON)
// isteklerini kaydeder; photoStatus sendPhoto yanıt kodu.
func fakeTelegram(t *testing.T, photoStatus int) (*[]tgCall, *sync.Mutex) {
	t.Helper()
	var calls []tgCall
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := tgCall{path: r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]}
		if c.path == "sendPhoto" {
			_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil {
				t.Errorf("içerik türü: %v", err)
			}
			mr := multipart.NewReader(r.Body, params["boundary"])
			for {
				p, err := mr.NextPart()
				if err != nil {
					break
				}
				b, _ := io.ReadAll(p)
				switch p.FormName() {
				case "caption":
					c.caption = string(b)
				case "photo":
					c.photo = b
				}
			}
		} else {
			b, _ := io.ReadAll(r.Body)
			c.text = string(b)
		}
		mu.Lock()
		calls = append(calls, c)
		mu.Unlock()
		if c.path == "sendPhoto" && photoStatus != 200 {
			w.WriteHeader(photoStatus)
		}
	}))
	t.Cleanup(srv.Close)
	old := telegramAPI
	telegramAPI = srv.URL
	t.Cleanup(func() { telegramAPI = old })
	return &calls, &mu
}

var pngStub = []byte("\x89PNG\r\n\x1a\nveri")

func TestTelegramPhoto(t *testing.T) {
	cfg := map[string]any{"bot_token": "1:A", "chat_id": "-100"}
	ev := downEvent
	ev.Image = pngStub

	// kısa metin: tek sendPhoto, altyazı = tam metin
	calls, _ := fakeTelegram(t, 200)
	if err := send(t, "telegram", cfg, ev); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0].path != "sendPhoto" || (*calls)[0].caption != ev.Text() || string((*calls)[0].photo) != string(pngStub) {
		t.Fatalf("fotoğraf yolu: %+v", *calls)
	}

	// uzun metin: kısa altyazılı fotoğraf + tam metin
	calls, _ = fakeTelegram(t, 200)
	long := ev
	long.Message = strings.Repeat("uzun ayrıntı ", 120)
	if err := send(t, "telegram", cfg, long); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 2 || (*calls)[0].path != "sendPhoto" || (*calls)[1].path != "sendMessage" {
		t.Fatalf("uzun metin yolu: %+v", *calls)
	}
	if n := utf16Len((*calls)[0].caption); n == 0 || n > telegramCaptionMax || strings.Contains((*calls)[0].caption, "\n") {
		t.Fatalf("altyazı: %q", (*calls)[0].caption)
	}
	if !strings.Contains((*calls)[1].text, "uzun ayrıntı uzun ayrıntı") {
		t.Fatalf("tam metin gitmedi: %s", (*calls)[1].text)
	}

	// sendPhoto 500: metin sendMessage ile gider
	calls, _ = fakeTelegram(t, 500)
	if err := send(t, "telegram", cfg, ev); err != nil {
		t.Fatalf("metne düşmeliydi: %v", err)
	}
	if len(*calls) != 2 || (*calls)[1].path != "sendMessage" || !strings.Contains((*calls)[1].text, `"chat_id":"-100"`) {
		t.Fatalf("yedek yol: %+v", *calls)
	}

	// görüntü yok: yalnız sendMessage
	calls, _ = fakeTelegram(t, 200)
	if err := send(t, "telegram", cfg, downEvent); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0].path != "sendMessage" {
		t.Fatalf("metin yolu: %+v", *calls)
	}
}

func TestTelegramCaption(t *testing.T) {
	if c, f := telegramCaption("kısa"); c != "kısa" || f != "" {
		t.Fatal(c, f)
	}
	head := strings.Repeat("ş", 1500)
	c, f := telegramCaption(head + "\nikinci")
	if utf16Len(c) != telegramCaptionMax || !strings.HasSuffix(c, "…") || f == "" {
		t.Fatalf("kısaltma: %d %q", utf16Len(c), c[len(c)-6:])
	}
}
