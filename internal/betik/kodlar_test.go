package betik

import (
	"os"
	"regexp"
	"testing"
)

// i18nBlock web/src/lib/i18n/<dil>/scripts.ts içinde "  ad: {" bloğunun anahtarlarını döner.
func i18nBlock(t *testing.T, lang, block string) map[string]bool {
	t.Helper()
	b, err := os.ReadFile("../../web/src/lib/i18n/" + lang + "/scripts.ts")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?ms)^  ` + block + `: \{\n(.*?)^  \},`).FindSubmatch(b)
	if m == nil {
		t.Fatalf("%s: %s bloğu yok", lang, block)
	}
	keys := map[string]bool{}
	for _, k := range regexp.MustCompile(`(?m)^    ([a-z_]+):`).FindAllSubmatch(m[1], -1) {
		keys[string(k[1])] = true
	}
	return keys
}

// Ajanın gönderebileceği her kodun TR ve EN metni olmalı; yoksa arayüz ham kod gösterir.
func TestRedKodlariCevrili(t *testing.T) {
	codes := append([]string{NoticeClockForward, NoticeQueueDropped}, AllCodes...)
	reasons := []string{ReasonMissing, ReasonDisabled, ReasonConfig, ReasonState, ReasonPlatform, ReasonInsecure}
	for _, lang := range []string{"tr", "en"} {
		got := i18nBlock(t, lang, "codes")
		for _, c := range codes {
			if !got[c] {
				t.Errorf("%s: codes.%s çevirisi yok", lang, c)
			}
		}
		got = i18nBlock(t, lang, "reason")
		for _, r := range reasons {
			if !got[r] {
				t.Errorf("%s: reason.%s çevirisi yok", lang, r)
			}
		}
	}
}
