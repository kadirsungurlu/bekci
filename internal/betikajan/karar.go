package betikajan

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"slices"

	"github.com/kadirsungurlu/bekci/internal/betik"
)

// clockBackTolerance küçük NTP düzeltmeleri kilitlemesin; bilet ömrünün (15 dk)
// çok altındadır.
const clockBackTolerance = 120

// ticketSkew operatör makinesiyle sunucu saati arasındaki pay.
const ticketSkew = 60

// maxRevokeIDs iptal kaydı üst sınırı (sızan anahtar durumu şişiremesin).
const maxRevokeIDs = 1024

// reject kodlu red; panele yalnız code gider.
type reject struct {
	code   string
	script string
	detail string
}

func (r *reject) Error() string { return r.code + ": " + r.detail }

func refuse(code, detail string) *reject { return &reject{code: code, detail: detail} }

// prepared doğrulanmış, çalışmaya hazır iş.
type prepared struct {
	runID    string
	m        *betik.Manifest
	body     []byte
	env      map[string]string
	eff      string // etkin onay
	manifest string // manifest_sha256
	params   string // params_sha256
	ticket   string // ticket_sha256
	expiry   int64  // run_id budanma zamanı
}

// EffectiveApproval en yüksek(imzalı, yerel taban, iğne yoksa signed). Yerel
// değerler imzayla düşürülemez.
func EffectiveApproval(signed string, a *Allow) string {
	eff := betik.MaxApproval(signed, a.MinApproval)
	if a.SHA256 == "" {
		return betik.ApprovalSigned
	}
	return eff
}

func checkClock(st *State, now int64) *reject {
	if st.LastSeen > 0 && now < st.LastSeen-clockBackTolerance {
		return refuse(betik.CodeClockBack, "saat last_seen'den geri")
	}
	return nil
}

// prepare §6 adım 2-6: ilk hatada reddeder, durumu değiştirmez. Sıra önemlidir:
// imza doğrulanmadan hiçbir alan ayrıştırılmaz.
func prepare(job *betik.RunJob, cfg *Config, hostKey string, st *State, now int64) (*prepared, *reject) {
	if r := checkClock(st, now); r != nil {
		return nil, r
	}
	// Adım 2: bildirim.
	env, err := betik.ParseEnvelope(job.Manifest)
	if err != nil {
		return nil, refuse(betik.CodeEnvelopeInvalid, err.Error())
	}
	if !env.VerifyAny(betik.KindManifest, cfg.yayin) {
		return nil, refuse(betik.CodeSigInvalid, "bildirim imzası")
	}
	m, err := betik.ParseManifest(env.Payload)
	if errors.Is(err, betik.ErrSchema) {
		return nil, refuse(betik.CodeSchemaUnknown, "bildirim")
	}
	if err != nil {
		return nil, refuse(betik.CodePayloadInvalid, err.Error())
	}
	script := func(r *reject) (*prepared, *reject) { r.script = m.ID; return nil, r }
	if now < m.NotBefore {
		return script(refuse(betik.CodeNotYet, "not_before"))
	}
	if now > m.NotAfter {
		return script(refuse(betik.CodeExpired, "not_after"))
	}
	if m.Version < st.Revokes[m.ID] {
		return script(refuse(betik.CodeRevoked, "iptal"))
	}
	floor := int64(0)
	if s := st.Scripts[m.ID]; s != nil {
		floor = s.Floor
	}
	if m.Version < floor {
		return script(refuse(betik.CodeVersionRollback, "sürüm tabanı"))
	}
	if m.Version > floor+betik.MaxVersionJump {
		return script(refuse(betik.CodeVersionJump, "sürüm artışı"))
	}
	// Adım 3: gövde, grup, yerel izin, iğne.
	if len(job.Body) > betik.MaxBody {
		return script(refuse(betik.CodeBodyTooLarge, "gövde"))
	}
	if betik.Digest(job.Body) != m.SHA256 {
		return script(refuse(betik.CodeBodyMismatch, "gövde özeti"))
	}
	if !slices.ContainsFunc(m.Groups, func(g string) bool { return slices.Contains(cfg.Groups, g) }) {
		return script(refuse(betik.CodeGroupDenied, "grup"))
	}
	allow := cfg.allow(m.ID)
	if allow == nil {
		return script(refuse(betik.CodeNotAllowed, "yerel izin"))
	}
	if allow.SHA256 != "" && allow.SHA256 != m.SHA256 {
		return script(refuse(betik.CodePinMismatch, "iğne"))
	}
	p := &prepared{
		runID: job.RunID, m: m, body: job.Body, eff: EffectiveApproval(m.Approval, allow),
		manifest: env.PayloadDigest(), params: betik.Digest(job.Params), expiry: m.NotAfter,
	}
	// Adım 4: bilet. Geldiyse sınıf ne olursa olsun doğrulanır (D3).
	if p.eff == betik.ApprovalSigned && len(job.Ticket) == 0 {
		return script(refuse(betik.CodeTicketMissing, "bilet yok"))
	}
	if len(job.Ticket) > 0 {
		digest, r := checkTicket(job, m, p, cfg.onay, hostKey, now)
		if r != nil {
			return script(r)
		}
		p.ticket = digest
	}
	// Adım 5: parametreler ve run_id.
	vars, err := betik.ParseParams(m, job.Params)
	if err != nil {
		return script(refuse(betik.CodeParamsInvalid, err.Error()))
	}
	p.env = vars
	p.env["BEKCI_RUN_ID"] = job.RunID
	if _, seen := st.Runs[job.RunID]; seen {
		return script(refuse(betik.CodeRunIDReplay, "run_id görüldü"))
	}
	// Adım 6: hız. Yalnız commit sayar, geçersiz istek kota tüketmez.
	if r := checkRates(st, cfg, p, now); r != nil {
		return script(r)
	}
	return p, nil
}

func checkTicket(job *betik.RunJob, m *betik.Manifest, p *prepared, keys []ed25519.PublicKey, hostKey string, now int64) (string, *reject) {
	env, err := betik.ParseEnvelope(job.Ticket)
	if err != nil {
		return "", refuse(betik.CodeTicketInvalid, err.Error())
	}
	if !env.VerifyAny(betik.KindTicket, keys) {
		return "", refuse(betik.CodeTicketSig, "bilet imzası")
	}
	t, err := betik.ParseTicket(env.Payload)
	if errors.Is(err, betik.ErrSchema) {
		return "", refuse(betik.CodeSchemaUnknown, "bilet")
	}
	if err != nil {
		return "", refuse(betik.CodeTicketInvalid, err.Error())
	}
	if t.HostKey != hostKey {
		return "", refuse(betik.CodeTicketHost, "başka ajan")
	}
	if t.RunID != job.RunID || t.ScriptID != m.ID || t.Version != m.Version ||
		t.ManifestSHA256 != p.manifest || t.ParamsSHA256 != p.params {
		return "", refuse(betik.CodeTicketMismatch, "bağlı alan tutmuyor")
	}
	if now > t.ExpiresAt || now < t.IssuedAt-ticketSkew {
		return "", refuse(betik.CodeTicketExpired, "bilet süresi")
	}
	return env.PayloadDigest(), nil
}

func checkRates(st *State, cfg *Config, p *prepared, now int64) *reject {
	s := st.Scripts[p.m.ID]
	if s != nil {
		if p.m.MinIntervalSec > 0 && s.LastRun > 0 && now-s.LastRun < p.m.MinIntervalSec {
			return refuse(betik.CodeRateInterval, "min_interval_sec")
		}
		if s.DayNum == now/86400 && s.DayCount >= p.m.MaxPerDay {
			return refuse(betik.CodeRateDay, "max_per_day")
		}
	}
	if now-st.Hour.Start < 3600 {
		used := st.Hour.Normal
		if p.eff == betik.ApprovalSigned {
			used = st.Hour.Signed
		}
		if used >= cfg.hourlyQuota() {
			return refuse(betik.CodeRateHour, "saatlik kota")
		}
	}
	return nil
}

// commit §6 adım 7: run_id, sayaç ve last_seen; Update hepsini tek atomik yazımla
// kaydeder. Bu yazım başarısızsa iş çalışmaz (en çok bir kez).
func commit(st *State, p *prepared, now int64) {
	for id, exp := range st.Runs {
		if exp < now {
			delete(st.Runs, id)
		}
	}
	st.Runs[p.runID] = p.expiry
	s := st.script(p.m.ID)
	s.LastRun = now
	if day := now / 86400; s.DayNum != day {
		s.DayNum, s.DayCount = day, 0
	}
	s.DayCount++
	if now-st.Hour.Start >= 3600 {
		st.Hour = HourWindow{Start: now}
	}
	if p.eff == betik.ApprovalSigned {
		st.Hour.Signed++
	} else {
		st.Hour.Normal++
	}
	if now > st.LastSeen {
		st.LastSeen = now
	}
	st.Active[p.runID] = &ActiveRun{
		ScriptID: p.m.ID, Version: p.m.Version, Manifest: p.manifest,
		Params: p.params, Ticket: p.ticket, AcceptedAt: now,
	}
}

// ApplyRevoke iptal zarfını doğrular ve duruma işler. Sızan anahtar sayacı
// kalıcı bozamasın diye artış sınırlıdır; yeniden oynatma etkisizdir.
func ApplyRevoke(cfg *Config, st *State, raw []byte) error {
	env, err := betik.ParseEnvelope(raw)
	if err != nil {
		return err
	}
	if !env.VerifyAny(betik.KindRevoke, cfg.yayin) {
		return errors.New("iptal imzası geçersiz")
	}
	r, err := betik.ParseRevoke(env.Payload)
	if err != nil {
		return err
	}
	base := st.Revokes[r.ID]
	if s := st.Scripts[r.ID]; s != nil && s.Floor > base {
		base = s.Floor
	}
	if r.BelowVersion > base+betik.MaxVersionJump {
		return fmt.Errorf("iptal sürümü sınır dışı: %d", r.BelowVersion)
	}
	if r.BelowVersion <= st.Revokes[r.ID] {
		return nil
	}
	if _, ok := st.Revokes[r.ID]; !ok && len(st.Revokes) >= maxRevokeIDs {
		return errors.New("iptal kaydı dolu")
	}
	st.Revokes[r.ID] = r.BelowVersion
	return nil
}
