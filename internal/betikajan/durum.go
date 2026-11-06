package betikajan

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/kadirsungurlu/bekci/internal/betik"
)

const (
	stateFile   = "durum.json"
	lockName    = "durum.kilit"
	keyFile     = "ajan.key"
	reserveFile = "rezerv"
	queueDir    = "kuyruk"
	reserveSize = 1 << 20
)

// ScriptState betik başına kalıcı sayaçlar.
type ScriptState struct {
	Floor    int64 `json:"floor"`    // başarıyla çalışan en yüksek version
	LastRun  int64 `json:"last_run"` // unix sn
	DayNum   int64 `json:"day_num"`  // unix gün (UTC)
	DayCount int   `json:"day_count"`
}

// HourWindow yerel saatlik kota. signed sayacı ayrıdır: düşük sınıf iş kuyruğu
// acil betiği kilitleyemez.
type HourWindow struct {
	Start  int64 `json:"start"`
	Normal int   `json:"normal"`
	Signed int   `json:"signed"`
}

// ActiveRun kaydı atılmış, makbuzu yazılmamış çalıştırma. Ajan ölürse açılışta
// buradan "lost" makbuzu üretilir.
type ActiveRun struct {
	ScriptID   string `json:"script_id"`
	Version    int64  `json:"version"`
	Manifest   string `json:"manifest_sha256"`
	Params     string `json:"params_sha256"`
	Ticket     string `json:"ticket_sha256"`
	AcceptedAt int64  `json:"accepted_at"`
}

// State durum.json içeriği.
type State struct {
	Scripts  map[string]*ScriptState `json:"scripts"`
	Revokes  map[string]int64        `json:"revokes"`
	Runs     map[string]int64        `json:"runs"` // run_id -> budanabileceği zaman
	Active   map[string]*ActiveRun   `json:"active"`
	Hour     HourWindow              `json:"hour"`
	LastSeen int64                   `json:"last_seen"`
	Seq      int64                   `json:"seq"`
	Prev     string                  `json:"prev"`
}

func newState() *State {
	return &State{
		Scripts: map[string]*ScriptState{}, Revokes: map[string]int64{},
		Runs: map[string]int64{}, Active: map[string]*ActiveRun{},
	}
}

func (s *State) script(id string) *ScriptState {
	v := s.Scripts[id]
	if v == nil {
		v = &ScriptState{}
		s.Scripts[id] = v
	}
	return v
}

type stateWire struct {
	Sum   string          `json:"sum"`
	State json.RawMessage `json:"state"`
}

// Store durum dosyası. Her değişiklik kilit altında oku-değiştir-yaz yapılır:
// yerel komutlar da aynı dosyaya yazar, bellekte kopya tutulmaz.
type Store struct {
	dir   string
	write func(path string, data []byte, perm os.FileMode) error
}

// NewStore dizindeki durumu kullanır.
func NewStore(dir string) *Store { return &Store{dir: dir, write: atomicWrite} }

func (s *Store) path(n string) string { return filepath.Join(s.dir, n) }

// ErrStateWrite durum yazılamadı: iş çalıştırılmaz.
var ErrStateWrite = errors.New("durum yazılamadı")

func (s *Store) load() (*State, error) {
	data, err := readTrusted(s.path(stateFile), 0o077, 64<<20)
	if err != nil {
		return nil, fmt.Errorf("durum okunamadı: %w", err)
	}
	var w stateWire
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, fmt.Errorf("durum bozuk: %w", err)
	}
	if w.Sum == "" || w.Sum != betik.Digest(w.State) {
		return nil, errors.New("durum özeti tutmuyor")
	}
	st := newState()
	if err := json.Unmarshal(w.State, st); err != nil {
		return nil, fmt.Errorf("durum bozuk: %w", err)
	}
	if st.Scripts == nil || st.Revokes == nil || st.Runs == nil || st.Active == nil {
		return nil, errors.New("durum eksik")
	}
	return st, nil
}

func encodeState(st *State) []byte {
	raw := mustJSON(st)
	var b bytes.Buffer
	fmt.Fprintf(&b, `{"sum":%q,"state":`, betik.Digest(raw))
	b.Write(raw)
	b.WriteString("}\n")
	return b.Bytes()
}

// save atomik yazar. Disk doluysa açılışta ayrılan alan bırakılıp bir kez daha
// denenir: dolu disk temizliği tam da bu durumda gerekir.
func (s *Store) save(st *State) error {
	data := encodeState(st)
	err := s.write(s.path(stateFile), data, 0o600)
	if errors.Is(err, syscall.ENOSPC) {
		os.Remove(s.path(reserveFile))
		err = s.write(s.path(stateFile), data, 0o600)
	}
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStateWrite, err)
	}
	return nil
}

// Read durumu kilitle okur.
func (s *Store) Read() (*State, error) {
	unlock, err := lockFile(s.path(lockName))
	if err != nil {
		return nil, err
	}
	defer unlock()
	return s.load()
}

// Update durumu kilit altında okur, fn hatasızsa yazar. fn hata dönerse hiçbir
// şey yazılmaz: geçersiz istek kota tüketmez.
func (s *Store) Update(fn func(*State) error) error {
	unlock, err := lockFile(s.path(lockName))
	if err != nil {
		return err
	}
	defer unlock()
	st, err := s.load()
	if err != nil {
		return err
	}
	if err := fn(st); err != nil {
		return err
	}
	return s.save(st)
}

// EnsureReserve disk dolunca durum yazılabilsin diye küçük bir alan ayırır.
func (s *Store) EnsureReserve() {
	p := s.path(reserveFile)
	if fi, err := os.Stat(p); err == nil && fi.Size() == reserveSize {
		return
	}
	s.write(p, make([]byte, reserveSize), 0o600)
}

// LoadKey K_ajan özel anahtarını okur (0600, yalnız sahibi).
func (s *Store) LoadKey() (ed25519.PrivateKey, error) {
	data, err := readTrusted(s.path(keyFile), 0o077, 4096)
	if err != nil {
		return nil, err
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, errors.New("ajan anahtarı bozuk")
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// EnsureKey anahtar yoksa üretir, varsa korur.
func (s *Store) EnsureKey() (ed25519.PrivateKey, error) {
	if k, err := s.LoadKey(); err == nil {
		return k, nil
	}
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return nil, err
	}
	enc := []byte(base64.StdEncoding.EncodeToString(seed) + "\n")
	if err := s.write(s.path(keyFile), enc, 0o600); err != nil {
		return nil, err
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // yalnız sabit yapılar kodlanır
	}
	return b
}

// atomicWrite geçici dosya + fsync + rename + dizin fsync.
func atomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".gecici-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(data); err == nil {
		err = f.Chmod(perm)
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
	}
	return nil
}

// HostKey anahtarın host_key değeri.
func HostKey(k ed25519.PrivateKey) string {
	return betik.KeyID(k.Public().(ed25519.PublicKey))
}
