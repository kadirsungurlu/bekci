package store

import (
	"context"
	"time"
)

// Kullanıcıların yedeğe alınması ve geri yüklenmesi (#12). Şifre özeti
// (bcrypt) olduğu gibi taşınır; TOTP sırrı ve kurtarma kodu özetleri yalnızca
// dışa aktarmada istenirse dosyaya girer. Oturumlar ve API anahtarları
// yedeğe girmez (yeni kurulumda yeniden açılır).

// UserBackup bir kullanıcının yedek kaydı (API katmanı dosya biçimine çevirir).
type UserBackup struct {
	User          User
	TOTPSecret    string   // boş: 2FA kapalı ya da yedeğe alınmadı
	RecoveryCodes []string // kullanılmamış kurtarma kodu özetleri
}

// UsersForBackup tüm kullanıcılar ve istenirse 2FA sırları.
func (s *Store) UsersForBackup(ctx context.Context, withSecrets bool) ([]UserBackup, error) {
	users, err := s.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]UserBackup, 0, len(users))
	for _, u := range users {
		ub := UserBackup{User: u}
		if withSecrets && u.TwoFactorEnabled {
			if ub.TOTPSecret, _, err = s.TOTPSecret(ctx, u.ID); err != nil {
				return nil, err
			}
			rows, err := s.db.QueryContext(ctx, "SELECT code_hash FROM recovery_codes WHERE user_id = ? AND used_at IS NULL ORDER BY id", u.ID)
			if err != nil {
				return nil, err
			}
			for rows.Next() {
				var h string
				if err := rows.Scan(&h); err != nil {
					rows.Close()
					return nil, err
				}
				ub.RecoveryCodes = append(ub.RecoveryCodes, h)
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return nil, err
			}
		}
		out = append(out, ub)
	}
	return out, nil
}

// ImportUser yedekten kullanıcı ekler (kullanıcı adı yoksa). monitorIDs
// kısıtlı kullanıcının açık monitör seçimi (yeni kimliklerle), rules etiket
// kuralları. TOTPSecret doluysa 2FA açık olarak kurulur. İşlem içinde çağrılır.
func importUserTx(ctx context.Context, tx *Tx, ub UserBackup, monitorIDs []int64, rules []TagRule, now int64) (int64, error) {
	u := ub.User
	id, err := insertID(ctx, tx, `
		INSERT INTO users (username, display_name, email, role, disabled, must_change_password, all_monitors, password_hash,
			created_at, lang, theme, totp_secret, totp_enabled)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.Username, u.DisplayName, NormalizeEmail(u.Email), u.Role, boolInt(u.Disabled), boolInt(u.MustChangePassword),
		boolInt(u.AllMonitors), u.PasswordHash, now, u.Lang, u.Theme, ub.TOTPSecret, boolInt(ub.TOTPSecret != ""))
	if err != nil {
		return 0, err
	}
	if !u.AllMonitors {
		if err := setUserMonitors(ctx, tx, id, monitorIDs); err != nil {
			return 0, err
		}
		if err := setTagRulesTx(ctx, tx, "user_tags", "user_id", id, rules); err != nil {
			return 0, err
		}
	}
	if ub.TOTPSecret != "" && len(ub.RecoveryCodes) > 0 {
		if err := replaceRecoveryCodes(ctx, tx, id, ub.RecoveryCodes); err != nil {
			return 0, err
		}
	}
	return id, nil
}

// ImportUserData içe aktarılacak kullanıcı: monitör başvuruları dosya kimliğidir.
type ImportUserData struct {
	Backup     UserBackup
	MonitorIDs []int64 // dosya kimlikleri (IDMap ile çevrilir)
	TagRules   []ImportTagRef
}

// importUsersTx Import içinde: dosya kimliklerini çevirip kullanıcıları ekler.
func importUsersTx(ctx context.Context, tx *Tx, users []ImportUserData, idMap map[int64]int64, tagIDs []int64, now int64) ([]int64, error) {
	out := make([]int64, 0, len(users))
	for _, iu := range users {
		var mons []int64
		for _, fid := range iu.MonitorIDs {
			if id, ok := idMap[fid]; ok {
				mons = append(mons, id)
			}
		}
		var rules []TagRule
		for _, r := range iu.TagRules {
			tid := r.Tag.ID
			if r.Tag.New >= 0 {
				tid = tagIDs[r.Tag.New]
			}
			rules = append(rules, TagRule{TagID: tid, Value: r.Value})
		}
		id, err := importUserTx(ctx, tx, iu.Backup, mons, rules, now)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

// nowUnix test edilebilirlik için ayrı.
func nowUnix() int64 { return time.Now().Unix() }
