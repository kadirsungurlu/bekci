package probe

// Kontrol noktaları arası TCP ping çekirdeği (mesh).
//
// Her eş çifti için TEK kalıcı TCP bağlantısı kurulur; iki taraf da kendi
// ping'lerini PingInterval aralığında aynı bağlantıdan gönderir, karşı taraf
// pong döner. RTT ve kayıp her taraf için kendi ping'lerine göre ölçülür.
//
// Çerçeve (TLS yok, bütünlük HMAC ile):
//
//	uzunluk  uint32 BE  (gövde boyu, en çok meshMaxBody)
//	gövde:
//	  tür       uint8   (1 hello, 2 ping, 3 pong; yeni türler eklenebilir)
//	  bayrak    uint8   (ayrılmış, ileride "gönderdim" vb. işaretler için)
//	  gönderen  int64 BE
//	  alıcı     int64 BE
//	  sıra no   uint64 BE (yön başına kesin artan)
//	  zaman     int64 BE (unix nano)
//	  yük boyu  uint16 BE
//	  yük       []byte  (pong: yanıtlanan ping'in sıra no'su, 8 bayt)
//	  hmac      32 bayt  HMAC-SHA256(çift anahtarı, gövdenin hmac öncesi kısmı)

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"sync"
	"time"
)

const (
	meshMaxBody       = 4096
	meshMaxSkew       = 5 * time.Minute
	meshMacLen        = sha256.Size
	meshHeaderLen     = 1 + 1 + 8 + 8 + 8 + 8 + 2
	meshMaxBackoff    = 60 * time.Second
	meshHelloTimeout  = 5 * time.Second
	meshDefaultPing   = 5 * time.Second
	meshDefaultLoss   = 3
	meshStateUp       = "up"
	meshStateDown     = "down"
	meshStateUnknown  = "unknown"
	meshMinIdleWindow = 2 * time.Second
)

type meshFrameType uint8

const (
	meshHello meshFrameType = 1
	meshPing  meshFrameType = 2
	meshPong  meshFrameType = 3
)

var (
	errMeshTooLarge = errors.New("mesh: çerçeve 4 KB sınırını aşıyor")
	errMeshShort    = errors.New("mesh: çerçeve eksik")
)

type meshFrame struct {
	Type    meshFrameType
	Flags   uint8
	From    int64
	To      int64
	Seq     uint64
	TS      int64
	Payload []byte
}

// encodeMeshFrame uzunluk önekli, HMAC'li çerçeveyi üretir.
func encodeMeshFrame(f meshFrame, key []byte) ([]byte, error) {
	bodyLen := meshHeaderLen + len(f.Payload) + meshMacLen
	if bodyLen > meshMaxBody {
		return nil, errMeshTooLarge
	}
	buf := make([]byte, 4+bodyLen)
	binary.BigEndian.PutUint32(buf, uint32(bodyLen))
	b := buf[4:]
	b[0] = byte(f.Type)
	b[1] = f.Flags
	binary.BigEndian.PutUint64(b[2:], uint64(f.From))
	binary.BigEndian.PutUint64(b[10:], uint64(f.To))
	binary.BigEndian.PutUint64(b[18:], f.Seq)
	binary.BigEndian.PutUint64(b[26:], uint64(f.TS))
	binary.BigEndian.PutUint16(b[34:], uint16(len(f.Payload)))
	copy(b[meshHeaderLen:], f.Payload)
	n := meshHeaderLen + len(f.Payload)
	mac := hmac.New(sha256.New, key)
	mac.Write(b[:n])
	copy(b[n:], mac.Sum(nil))
	return buf, nil
}

// readMeshBody bir çerçevenin gövdesini okur; sınırı aşan ya da eksik çerçeve hatadır.
func readMeshBody(r io.Reader) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n > meshMaxBody {
		return nil, errMeshTooLarge
	}
	if n < meshHeaderLen+meshMacLen {
		return nil, errMeshShort
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}

// parseMeshFrame başlığı çözer (HMAC doğrulamaz).
func parseMeshFrame(body []byte) (meshFrame, error) {
	if len(body) < meshHeaderLen+meshMacLen {
		return meshFrame{}, errMeshShort
	}
	plen := int(binary.BigEndian.Uint16(body[34:]))
	if meshHeaderLen+plen+meshMacLen != len(body) {
		return meshFrame{}, errMeshShort
	}
	return meshFrame{
		Type:    meshFrameType(body[0]),
		Flags:   body[1],
		From:    int64(binary.BigEndian.Uint64(body[2:])),
		To:      int64(binary.BigEndian.Uint64(body[10:])),
		Seq:     binary.BigEndian.Uint64(body[18:]),
		TS:      int64(binary.BigEndian.Uint64(body[26:])),
		Payload: body[meshHeaderLen : meshHeaderLen+plen],
	}, nil
}

func verifyMeshMAC(body, key []byte) bool {
	n := len(body) - meshMacLen
	mac := hmac.New(sha256.New, key)
	mac.Write(body[:n])
	return hmac.Equal(mac.Sum(nil), body[n:])
}

// MeshConfig mesh çekirdeği ayarları.
type MeshConfig struct {
	SelfID        int64
	ListenAddr    string        // "" = dinleme yok; ör. ":7480", testte "127.0.0.1:0"
	PingInterval  time.Duration // panelden, vars. 5 sn
	LossThreshold int           // ardışık kayıp, vars. 3
	Now           func() time.Time
	Logf          func(format string, args ...any)
}

type meshRole int

const (
	meshRoleUnknown meshRole = iota // ölçülemiyor (iki taraf da dinlemiyor)
	meshRoleDial                    // bu taraf bağlanır
	meshRoleAccept                  // eş bağlanır, bu taraf bekler
)

// meshRoleFor çiftte kimin bağlanacağını belirler (iki taraf aynı sonuca varır).
func meshRoleFor(self int64, selfListening bool, p MeshPeer) meshRole {
	if self < p.ID {
		if p.Addr != "" {
			return meshRoleDial
		}
		if selfListening {
			return meshRoleAccept
		}
		return meshRoleUnknown
	}
	if selfListening {
		return meshRoleAccept
	}
	if p.Addr != "" {
		return meshRoleDial
	}
	return meshRoleUnknown
}

type meshPeer struct {
	peer   MeshPeer
	key    []byte
	role   meshRole
	connCh chan *meshSession
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	// Aşağıdakiler Mesh.mu ile korunur.
	started bool
	rep     MeshLinkReport
	cur     *meshSession
}

// Mesh eşlere TCP ping ölçümü yapan çekirdek.
type Mesh struct {
	cfg  MeshConfig
	now  func() time.Time
	logf func(format string, args ...any)

	mu            sync.Mutex
	ping          time.Duration
	loss          int
	selfListening bool
	peers         map[int64]*meshPeer
	helloSeq      map[int64]uint64 // eşten kabul edilen son hello sırası (yeniden oynatma koruması)
	sendSeq       map[int64]uint64 // eşe giden yönün son sıra no'su
	ctx           context.Context
	cancel        context.CancelFunc
	ln            net.Listener
	closed        bool
	wg            sync.WaitGroup
}

// NewMesh yeni mesh çekirdeği oluşturur; Start çağrılana kadar ağ işi yapmaz.
func NewMesh(cfg MeshConfig) *Mesh {
	m := &Mesh{
		cfg:      cfg,
		now:      cfg.Now,
		logf:     cfg.Logf,
		peers:    map[int64]*meshPeer{},
		helloSeq: map[int64]uint64{},
		sendSeq:  map[int64]uint64{},
	}
	if m.now == nil {
		m.now = time.Now
	}
	if m.logf == nil {
		m.logf = func(string, ...any) {}
	}
	m.ping, m.loss = normTiming(cfg.PingInterval, cfg.LossThreshold)
	return m
}

func normTiming(ping time.Duration, loss int) (time.Duration, int) {
	if ping <= 0 {
		ping = meshDefaultPing
	}
	if loss <= 0 {
		loss = meshDefaultLoss
	}
	return ping, loss
}

// Start dinleyiciyi (varsa) açar ve eş döngülerini başlatır.
func (m *Mesh) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("mesh: kapatılmış")
	}
	if m.ctx != nil {
		return errors.New("mesh: zaten başlatıldı")
	}
	var ln net.Listener
	if m.cfg.ListenAddr != "" {
		var err error
		ln, err = net.Listen("tcp", m.cfg.ListenAddr)
		if err != nil {
			return fmt.Errorf("mesh dinleme: %w", err)
		}
	}
	m.ctx, m.cancel = context.WithCancel(ctx)
	if ln != nil {
		m.ln = ln
		context.AfterFunc(m.ctx, func() { ln.Close() })
		m.wg.Add(1)
		go m.acceptLoop(ln)
	}
	for _, p := range m.peers {
		m.startPeerLocked(p)
	}
	return nil
}

// ListenPort gerçek dinleme portunu döner; dinlemiyorsa 0.
func (m *Mesh) ListenPort() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ln == nil {
		return 0
	}
	if a, ok := m.ln.Addr().(*net.TCPAddr); ok {
		return a.Port
	}
	return 0
}

// SetTiming ping aralığını ve kayıp eşiğini günceller (sonraki tikten itibaren).
func (m *Mesh) SetTiming(ping time.Duration, loss int) {
	ping, loss = normTiming(ping, loss)
	m.mu.Lock()
	m.ping, m.loss = ping, loss
	for _, p := range m.peers {
		if p.rep.State != meshStateUnknown || p.rep.LossStreak > 0 {
			if p.rep.LossStreak >= loss {
				p.rep.State = meshStateDown
			} else if p.rep.State == meshStateDown {
				p.rep.State = meshStateUp
			}
		}
	}
	m.mu.Unlock()
}

func (m *Mesh) timing() (time.Duration, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ping, m.loss
}

// SetPeers eş listesini uygular; eklenen, çıkan, adresi/anahtarı ya da rolü
// değişen eşlerin döngüsü (ve bağlantısı) yeniden kurulur.
func (m *Mesh) SetPeers(selfListening bool, peers []MeshPeer) {
	var stop []*meshPeer
	m.mu.Lock()
	m.selfListening = selfListening
	seen := map[int64]bool{}
	for _, np := range peers {
		if np.ID == m.cfg.SelfID || seen[np.ID] {
			continue
		}
		seen[np.ID] = true
		key, err := hex.DecodeString(np.Key)
		if err != nil || len(key) == 0 {
			m.logf("mesh: eş %d anahtarı geçersiz, ölçülmeyecek", np.ID)
			key = nil
		}
		role := meshRoleUnknown
		if key != nil {
			role = meshRoleFor(m.cfg.SelfID, selfListening, np)
		}
		old := m.peers[np.ID]
		if old != nil && old.peer.Addr == np.Addr && old.peer.Key == np.Key && old.role == role {
			old.peer.Name = np.Name
			continue
		}
		p := &meshPeer{
			peer:   np,
			key:    key,
			role:   role,
			connCh: make(chan *meshSession),
			done:   make(chan struct{}),
			rep:    MeshLinkReport{Peer: np.ID, State: meshStateUnknown},
		}
		if old != nil {
			stop = append(stop, old)
			if role != meshRoleUnknown {
				p.rep = old.rep
			}
		}
		m.peers[np.ID] = p
		if m.ctx != nil && !m.closed {
			m.startPeerLocked(p)
		}
	}
	for id, old := range m.peers {
		if !seen[id] {
			stop = append(stop, old)
			delete(m.peers, id)
		}
	}
	m.mu.Unlock()
	for _, p := range stop {
		m.stopPeer(p)
	}
}

func (m *Mesh) startPeerLocked(p *meshPeer) {
	if p.started {
		return
	}
	p.started = true
	p.ctx, p.cancel = context.WithCancel(m.ctx)
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.runPeer(p)
	}()
}

func (m *Mesh) stopPeer(p *meshPeer) {
	m.mu.Lock()
	started := p.started
	m.mu.Unlock()
	if !started {
		return
	}
	p.cancel()
	<-p.done
}

// Report eş başına son durumu peer id sırasıyla döner.
func (m *Mesh) Report() []MeshLinkReport {
	m.mu.Lock()
	out := make([]MeshLinkReport, 0, len(m.peers))
	for _, p := range m.peers {
		out = append(out, p.rep)
	}
	m.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Peer < out[j].Peer })
	return out
}

// Close dinleyiciyi, bağlantıları ve tüm goroutine'leri kapatır.
func (m *Mesh) Close() {
	m.mu.Lock()
	m.closed = true
	if m.cancel != nil {
		m.cancel()
	}
	if m.ln != nil {
		m.ln.Close()
	}
	m.mu.Unlock()
	m.wg.Wait()
}

// sessionDialer eşle etkin bağlantıyı başlatan tarafın id'si; bağlantı yoksa 0.
func (m *Mesh) sessionDialer(peer int64) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p := m.peers[peer]; p != nil && p.cur != nil {
		return p.cur.dialer
	}
	return 0
}

func (m *Mesh) nextSeq(peer int64) uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.sendSeq[peer]
	if !ok {
		// Yeniden başlatmadan sonra da artan kalsın diye gerçek saatten başlar.
		v = uint64(time.Now().UnixNano())
	}
	v++
	m.sendSeq[peer] = v
	return v
}

func (m *Mesh) recordLoss(p *meshPeer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p.rep.LossStreak++
	if p.rep.LossStreak >= m.loss {
		p.rep.State = meshStateDown
	}
}

func (m *Mesh) recordOK(p *meshPeer, rtt time.Duration) {
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	p.rep.LossStreak = 0
	p.rep.State = meshStateUp
	p.rep.RTTms = rtt.Milliseconds()
	p.rep.LastOKAt = now.Unix()
}

func (m *Mesh) setCur(p *meshPeer, s *meshSession) {
	m.mu.Lock()
	p.cur = s
	m.mu.Unlock()
}

// ioTimeout yazma ve bağlanma süre sınırı.
func ioTimeout(ping time.Duration) time.Duration {
	return min(max(ping, time.Second), 10*time.Second)
}

// checkFrame gövdeyi doğrular: gönderen/alıcı, HMAC, sıra no, saat kayması.
func (m *Mesh) checkFrame(body, key []byte, peer int64, lastSeq uint64) (meshFrame, error) {
	f, err := parseMeshFrame(body)
	if err != nil {
		return f, err
	}
	if f.From != peer {
		return f, fmt.Errorf("beklenmeyen gönderen %d", f.From)
	}
	if f.To != m.cfg.SelfID {
		return f, fmt.Errorf("yanlış alıcı %d", f.To)
	}
	if !verifyMeshMAC(body, key) {
		return f, errors.New("HMAC doğrulanamadı")
	}
	if f.Seq <= lastSeq {
		return f, fmt.Errorf("eski ya da tekrar sıra no %d (son %d)", f.Seq, lastSeq)
	}
	skew := m.now().Sub(time.Unix(0, f.TS))
	if skew > meshMaxSkew || skew < -meshMaxSkew {
		return f, fmt.Errorf("saat kayması %s", skew.Round(time.Second))
	}
	return f, nil
}

func (m *Mesh) acceptLoop(ln net.Listener) {
	defer m.wg.Done()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if m.ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}
			m.logf("mesh: kabul hatası: %v", err)
			select {
			case <-m.ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
			continue
		}
		m.wg.Add(1)
		go m.handshake(conn)
	}
}

// handshake gelen bağlantının ilk çerçevesini (hello) doğrular ve eş döngüsüne verir.
func (m *Mesh) handshake(conn net.Conn) {
	defer m.wg.Done()
	stopAF := context.AfterFunc(m.ctx, func() { conn.Close() })
	defer stopAF()
	remote := conn.RemoteAddr().String()
	reject := func(format string, args ...any) {
		m.logf("mesh: gelen bağlantı reddedildi (%s): "+format, append([]any{remote}, args...)...)
		conn.Close()
	}
	conn.SetReadDeadline(time.Now().Add(meshHelloTimeout))
	body, err := readMeshBody(conn)
	if err != nil {
		reject("%v", err)
		return
	}
	hdr, err := parseMeshFrame(body)
	if err != nil {
		reject("%v", err)
		return
	}
	m.mu.Lock()
	p := m.peers[hdr.From]
	last := m.helloSeq[hdr.From]
	m.mu.Unlock()
	if p == nil || p.key == nil {
		reject("bilinmeyen eş %d", hdr.From)
		return
	}
	f, err := m.checkFrame(body, p.key, p.peer.ID, last)
	if err != nil {
		reject("eş %d: %v", hdr.From, err)
		return
	}
	if f.Type != meshHello {
		reject("eş %d: ilk çerçeve hello değil", hdr.From)
		return
	}
	m.mu.Lock()
	if f.Seq <= m.helloSeq[f.From] {
		m.mu.Unlock()
		reject("eş %d: tekrar hello", f.From)
		return
	}
	m.helloSeq[f.From] = f.Seq
	m.mu.Unlock()
	conn.SetReadDeadline(time.Time{})
	s := newMeshSession(m, p, conn, f.From)
	s.lastSeq = f.Seq
	select {
	case p.connCh <- s:
	case <-p.ctx.Done():
		s.close()
	}
}

type meshSession struct {
	m      *Mesh
	p      *meshPeer
	conn   net.Conn
	dialer int64
	done   chan struct{}
	once   sync.Once

	wmu sync.Mutex // yazma ve sıra no ataması

	mu      sync.Mutex // bekleyen ping
	waiting bool
	pendSeq uint64
	sentAt  time.Time

	lastSeq uint64 // yalnız okuyucu goroutine
}

func newMeshSession(m *Mesh, p *meshPeer, conn net.Conn, dialer int64) *meshSession {
	return &meshSession{m: m, p: p, conn: conn, dialer: dialer, done: make(chan struct{})}
}

func (s *meshSession) close() { s.once.Do(func() { s.conn.Close() }) }

func (s *meshSession) send(t meshFrameType, payload []byte) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	f := meshFrame{
		Type:    t,
		From:    s.m.cfg.SelfID,
		To:      s.p.peer.ID,
		Seq:     s.m.nextSeq(s.p.peer.ID),
		TS:      s.m.now().UnixNano(),
		Payload: payload,
	}
	buf, err := encodeMeshFrame(f, s.p.key)
	if err != nil {
		return err
	}
	if t == meshPing {
		s.mu.Lock()
		s.waiting, s.pendSeq, s.sentAt = true, f.Seq, time.Now()
		s.mu.Unlock()
	}
	ping, _ := s.m.timing()
	s.conn.SetWriteDeadline(time.Now().Add(ioTimeout(ping)))
	_, err = s.conn.Write(buf)
	return err
}

// readLoop gelen ping'lere pong döner, pong'lardan RTT ölçer.
func (s *meshSession) readLoop() {
	defer close(s.done)
	defer s.close()
	for {
		ping, loss := s.m.timing()
		s.conn.SetReadDeadline(time.Now().Add(max(ping*time.Duration(loss+2), meshMinIdleWindow)))
		body, err := readMeshBody(s.conn)
		if err != nil {
			if !errors.Is(err, net.ErrClosed) && !errors.Is(err, io.EOF) {
				s.m.logf("mesh: eş %d bağlantısı: %v", s.p.peer.ID, err)
			}
			return
		}
		f, err := s.m.checkFrame(body, s.p.key, s.p.peer.ID, s.lastSeq)
		if err != nil {
			s.m.logf("mesh: eş %d çerçevesi reddedildi, bağlantı kapatılıyor: %v", s.p.peer.ID, err)
			return
		}
		s.lastSeq = f.Seq
		switch f.Type {
		case meshPing:
			var pl [8]byte
			binary.BigEndian.PutUint64(pl[:], f.Seq)
			if err := s.send(meshPong, pl[:]); err != nil {
				return
			}
		case meshPong:
			if len(f.Payload) < 8 {
				s.m.logf("mesh: eş %d: pong yükü eksik", s.p.peer.ID)
				return
			}
			ack := binary.BigEndian.Uint64(f.Payload)
			s.mu.Lock()
			ok := s.waiting && ack == s.pendSeq
			rtt := time.Since(s.sentAt)
			if ok {
				s.waiting = false
			}
			s.mu.Unlock()
			if ok {
				s.m.recordOK(s.p, rtt)
			}
		case meshHello:
			s.m.logf("mesh: eş %d: oturum içinde beklenmeyen hello", s.p.peer.ID)
			return
		default:
			// Bilinmeyen tür: ileri uyumluluk için yok sayılır (HMAC doğrulandı).
		}
	}
}

func (m *Mesh) dial(ctx context.Context, p *meshPeer) *meshSession {
	ping, _ := m.timing()
	d := net.Dialer{Timeout: ioTimeout(ping)}
	conn, err := d.DialContext(ctx, "tcp", p.peer.Addr)
	if err != nil {
		if ctx.Err() == nil {
			m.logf("mesh: eş %d (%s) bağlanılamadı: %v", p.peer.ID, p.peer.Addr, err)
		}
		return nil
	}
	s := newMeshSession(m, p, conn, m.cfg.SelfID)
	if err := s.send(meshHello, nil); err != nil {
		m.logf("mesh: eş %d hello gönderilemedi: %v", p.peer.ID, err)
		s.close()
		return nil
	}
	return s
}

// runPeer bir eşin yönetim döngüsü: bağlanma/bekleme, ping tiki, kayıp sayımı.
func (m *Mesh) runPeer(p *meshPeer) {
	defer close(p.done)
	ctx := p.ctx
	var cur *meshSession
	var curDone <-chan struct{}
	dialCh := make(chan *meshSession)
	dialing := false
	var backoff time.Duration

	dialTimer := time.NewTimer(time.Hour)
	dialTimer.Stop()
	defer dialTimer.Stop()

	startDial := func() {
		if dialing || p.role != meshRoleDial {
			return
		}
		dialing = true
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			s := m.dial(ctx, p)
			select {
			case dialCh <- s:
			case <-ctx.Done():
				if s != nil {
					s.close()
				}
			}
		}()
	}
	scheduleDial := func() {
		if p.role != meshRoleDial {
			return
		}
		ping, _ := m.timing()
		limit := min(ping*4, meshMaxBackoff)
		if backoff == 0 {
			backoff = min(time.Second, limit)
		} else {
			backoff = min(backoff*2, limit)
		}
		dialTimer.Reset(backoff)
	}

	ping, _ := m.timing()
	tick := time.NewTimer(ping)
	defer tick.Stop()
	startDial()

	dropCur := func() {
		if cur != nil {
			cur.close()
			<-curDone
			cur, curDone = nil, nil
			m.setCur(p, nil)
		}
	}
	defer dropCur()

	for {
		select {
		case <-ctx.Done():
			return

		case s := <-dialCh:
			dialing = false
			if s == nil {
				if cur == nil {
					scheduleDial()
				}
				continue
			}
			m.adopt(p, &cur, &curDone, s, tick)
			backoff = 0

		case s := <-p.connCh:
			m.adopt(p, &cur, &curDone, s, tick)

		case <-curDone:
			cur, curDone = nil, nil
			m.setCur(p, nil)
			scheduleDial()

		case <-dialTimer.C:
			if cur == nil {
				startDial()
			}

		case <-tick.C:
			ping, _ := m.timing()
			tick.Reset(ping)
			if cur == nil {
				if p.role != meshRoleUnknown {
					m.recordLoss(p)
				}
				continue
			}
			cur.mu.Lock()
			lost := cur.waiting
			cur.mu.Unlock()
			if lost {
				m.recordLoss(p)
			}
			if err := cur.send(meshPing, nil); err != nil {
				cur.close() // okuyucu biter, curDone tetiklenir
			}
		}
	}
}

// adopt yeni bağlantıyı etkin bağlantı yapar; çift bağlantıda (yarış) yalnız biri
// kalır: aynı taraftan gelen yeni bağlantı eskisinin yerini alır, farklı
// taraflardan gelenlerde küçük id'nin başlattığı tutulur (iki taraf aynı seçer).
func (m *Mesh) adopt(p *meshPeer, cur **meshSession, curDone *<-chan struct{}, s *meshSession, tick *time.Timer) {
	if c := *cur; c != nil {
		keepNew := s.dialer == c.dialer || s.dialer == min(m.cfg.SelfID, p.peer.ID)
		if !keepNew {
			s.close()
			return
		}
		c.close()
		<-*curDone
	}
	*cur, *curDone = s, s.done
	m.setCur(p, s)
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		s.readLoop()
	}()
	ping, _ := m.timing()
	tick.Reset(ping)
	if err := s.send(meshPing, nil); err != nil {
		s.close()
	}
}
