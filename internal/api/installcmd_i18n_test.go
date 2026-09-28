package api

import (
	"strings"
	"testing"
)

// İngilizce kurulum komutlarında Türkçe mesaj kalmamalı; komutun kendisi
// (yollar, SHA, token) değişmemeli.
func TestLocalizeScript(t *testing.T) {
	s := &Server{}
	const sha = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	var cmds []string
	d, sd, w := s.serverSetupCommands("https://ornek.example", "upr_test")
	cmds = append(cmds, d, sd, w,
		windowsAgentCommand("https://ornek.example", "upr_test", sha),
		dockerInstallCommand(probeEnvPath, installEnvLines("https://ornek.example", "upr_test"), "docker run -d ", "", "uptime-probe-bin", sha, ""),
		dockerInstallCommand(probeEnvPath, installEnvLines("https://ornek.example", "upr_test"), "docker run -d ", "", "uptime-probe-bin", "", sha),
	)
	for i, c := range cmds {
		en := localizeScript("en", c)
		if strings.ContainsAny(en, "çğıöşüÇĞİÖŞÜ") {
			t.Errorf("komut %d İngilizcede Türkçe metin içeriyor:\n%s", i, en)
		}
		if localizeScript("tr", c) != c {
			t.Errorf("komut %d Türkçede değişmemeliydi", i)
		}
		for _, keep := range []string{"upr_test", "https://ornek.example"} {
			if strings.Contains(c, keep) && !strings.Contains(en, keep) {
				t.Errorf("komut %d çevirisinde %q kayboldu", i, keep)
			}
		}
	}
}
