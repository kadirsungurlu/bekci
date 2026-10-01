package store

import "testing"

func TestSettingsUserAgentValidation(t *testing.T) {
	s := DefaultSettings()
	s.CheckUserAgent = "  Bekci-Ozel/1  "
	if err := s.Validate(); err != nil || s.CheckUserAgent != "Bekci-Ozel/1" {
		t.Fatalf("geçerli değer: %q %v", s.CheckUserAgent, err)
	}
	for _, bad := range []string{"a\r\nX-Evil: 1", "türkçe", string(make([]byte, 301))} {
		s.CheckUserAgent = bad
		if s.Validate() == nil {
			t.Fatalf("reddedilmeliydi: %q", bad)
		}
	}
}
