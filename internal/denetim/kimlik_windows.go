//go:build windows

package denetim

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// izinDenetle Windows: sahip ve DACL'deki her izin girdisi yalnız SYSTEM ya da
// Administrators olmalı (ProgramData'dan miras gelen Users okuma izni red).
func izinDenetle(dir, path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 || !fi.Mode().IsRegular() {
		return hata(HataCredential, "%s düz dosya olmalı (bağ kabul edilmez)", path)
	}
	for _, p := range []string{dir, path} {
		if err := aclDenetle(p, p == dir); err != nil {
			return err
		}
	}
	return nil
}

func yetkili(sid *windows.SID) bool {
	return sid != nil && (sid.IsWellKnown(windows.WinLocalSystemSid) || sid.IsWellKnown(windows.WinBuiltinAdministratorsSid))
}

func aclDenetle(p string, klasor bool) error {
	sd, err := windows.GetNamedSecurityInfo(p, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return hata(HataCredential, "%s güvenlik bilgisi okunamadı: %v", p, err)
	}
	owner, _, err := sd.Owner()
	if err != nil || !yetkili(owner) {
		return hata(HataCredential, "%s sahibi SYSTEM ya da Administrators olmalı", p)
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		return hata(HataCredential, "%s erişim listesi (DACL) yok: herkes erişebilir", p)
	}
	for i := uint32(0); ; i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			break // girdiler bitti
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			continue // red girdileri erişimi daraltır
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !yetkili(sid) {
			what := "dosyaya"
			if klasor {
				what = "klasöre"
			}
			return hata(HataCredential, "%s: %s %s erişebiliyor; yalnız SYSTEM ve Administrators kalmalı (icacls %q /inheritance:r /grant:r SYSTEM:F Administrators:F)",
				p, sid.String(), what, p)
		}
	}
	return nil
}
