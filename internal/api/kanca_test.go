package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProKapi(t *testing.T) {
	s := &Server{}
	gecti := 0
	h := s.proKapi("filo", func(w http.ResponseWriter, r *http.Request) { gecti++ })
	istek := func(method string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h(w, httptest.NewRequest(method, "/api/pro/x", nil))
		return w
	}

	if w := istek(http.MethodGet); w.Code != http.StatusOK || gecti != 1 {
		t.Fatalf("lisanssız GET geçmeli: %d %d", w.Code, gecti)
	}
	if w := istek(http.MethodHead); w.Code != http.StatusOK || gecti != 2 {
		t.Fatalf("lisanssız HEAD geçmeli: %d", w.Code)
	}
	w := istek(http.MethodPost)
	if w.Code != http.StatusPaymentRequired || gecti != 2 || !strings.Contains(w.Body.String(), `"code":"pro_lisans"`) {
		t.Fatalf("lisanssız POST 402 olmalı: %d %s", w.Code, w.Body)
	}

	s.SetProOzellik(func(ad string) bool { return ad == "baska" })
	if w := istek(http.MethodDelete); w.Code != http.StatusPaymentRequired {
		t.Fatalf("başka özellik açıkken DELETE 402 olmalı: %d", w.Code)
	}
	s.SetProOzellik(func(ad string) bool { return ad == "filo" })
	if w := istek(http.MethodPost); w.Code != http.StatusOK || gecti != 3 {
		t.Fatalf("lisanslı POST geçmeli: %d", w.Code)
	}
}
