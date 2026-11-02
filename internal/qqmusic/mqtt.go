package qqmusic

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/net/websocket"
)

// The QQ Music app's QR login reports its scanning over MQTT 5, carried by
// a WebSocket: subscribed to the code's topic, the app hears it scanned,
// then the cookies of the login confirmed. As lyrune does, and with as
// little of MQTT as that takes.

const (
	mqttHost         = "mu.y.qq.com"
	mqttPath         = "/ws/handshake"
	mqttKeepAlive    = 45
	mqttWait         = 1500 * time.Millisecond // for an event, each poll
	mqttCallTimeout  = 6 * time.Second         // for a poll, retries and all
	mqttStepTimeout  = 5 * time.Second         // to connect, or subscribe
	mqttMaxRedirects = 3
)

type mqttKind int

const (
	mqttNone mqttKind = iota
	mqttScanned
	mqttExpired
	mqttCanceled
	mqttFailed
	mqttCookies
)

type mqttEvent struct {
	kind     mqttKind
	musicID  int64
	musicKey string
}

func (e mqttEvent) terminal() bool { return e.kind >= mqttExpired }

// mqttError is a failure of the connection: transient ones it connects
// again for.
type mqttError struct {
	msg       string
	transient bool
}

func (e *mqttError) Error() string { return "mqtt: " + e.msg }

func transientErr(format string, a ...any) error {
	return &mqttError{msg: fmt.Sprintf(format, a...), transient: true}
}

func fatalErr(format string, a ...any) error {
	return &mqttError{msg: fmt.Sprintf(format, a...)}
}

// mqttLogin follows the code id's login.
type mqttLogin struct {
	id string

	mu      sync.Mutex
	ws      *websocket.Conn
	buf     []byte
	pending *mqttEvent
	scanned bool // the code was scanned, as polls that hear nothing go on saying
	closed  bool
}

func newMQTTLogin(id string) *mqttLogin { return &mqttLogin{id: id} }

func (m *mqttLogin) close() {
	m.mu.Lock()
	m.closed = true
	ws := m.ws
	m.ws = nil
	m.mu.Unlock()
	if ws != nil {
		ws.Close()
	}
}

func (m *mqttLogin) drop() {
	m.mu.Lock()
	ws := m.ws
	m.ws, m.buf = nil, nil
	m.mu.Unlock()
	if ws != nil {
		ws.Close()
	}
}

// fallback is where the login is when nothing was heard.
func (m *mqttLogin) fallback() mqttEvent {
	if m.scanned {
		return mqttEvent{kind: mqttScanned}
	}
	return mqttEvent{kind: mqttNone}
}

// poll waits a little for what happens to the code, connecting again
// through failures for a few seconds.
func (m *mqttLogin) poll() (mqttEvent, error) {
	deadline := time.Now().Add(mqttCallTimeout)
	backoff := mqttWait
	for {
		ev, err := m.pollOnce()
		if err == nil {
			return ev, nil
		}
		var me *mqttError
		if !errors.As(err, &me) || !me.transient {
			m.drop()
			return mqttEvent{}, err
		}
		m.drop()
		if time.Now().After(deadline) {
			return m.fallback(), nil
		}
		time.Sleep(min(backoff, time.Until(deadline)))
		backoff = min(backoff*2, 3*time.Second)
	}
}

func (m *mqttLogin) pollOnce() (mqttEvent, error) {
	m.mu.Lock()
	closed, ws := m.closed, m.ws
	m.mu.Unlock()
	if closed {
		return mqttEvent{}, fatalErr("closed")
	}
	if ws == nil {
		var err error
		if ws, err = m.connect(); err != nil {
			return mqttEvent{}, err
		}
	}
	ev := m.pending
	m.pending = nil
	if ev == nil {
		got, err := m.next(ws, mqttWait)
		if err != nil {
			return mqttEvent{}, err
		}
		ev = got
	}
	if ev == nil || ev.kind == mqttNone {
		return m.fallback(), nil
	}
	if ev.kind == mqttScanned {
		m.scanned = true
	}
	if ev.terminal() {
		m.scanned = false
		m.drop()
	}
	return *ev, nil
}

// connect connects and subscribes to the code's topic, following the
// server where it sends the client on.
func (m *mqttLogin) connect() (*websocket.Conn, error) {
	ref := ""
	for range mqttMaxRedirects + 1 {
		path := mqttPath
		if ref != "" {
			path += "/" + ref
		}
		cfg, err := websocket.NewConfig("wss://"+mqttHost+":443"+path, "https://y.qq.com")
		if err != nil {
			return nil, fatalErr("%v", err)
		}
		cfg.Protocol = []string{"mqtt"}
		cfg.Header = http.Header{"Referer": {"https://y.qq.com/"}, "User-Agent": {UserAgent}}
		cfg.Dialer = &net.Dialer{Timeout: mqttStepTimeout}
		ws, err := websocket.DialConfig(cfg)
		if err != nil {
			return nil, transientErr("websocket connect failed: %v", err)
		}
		ws.PayloadType = websocket.BinaryFrame
		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()
			ws.Close()
			return nil, fatalErr("closed")
		}
		m.ws, m.buf = ws, nil
		m.mu.Unlock()
		next, err := m.handshake(ws)
		if err != nil {
			return nil, err
		}
		if next == "" {
			if err := m.subscribe(ws); err != nil {
				return nil, err
			}
			return ws, nil
		}
		m.drop()
		ref = next
	}
	return nil, fatalErr("too many redirects")
}

// handshake connects as the code's watcher, and returns the server to
// move to, if the one connected to sends the client on.
func (m *mqttLogin) handshake(ws *websocket.Conn) (string, error) {
	var props []byte
	props = append(props, 0x15)
	props = append(props, mqttString("pass")...)
	for _, kv := range [][2]string{{"tmeAppID", "qqmusic"}, {"business", "management"}, {"hashTag", m.id}, {"clientTag", "management.user"}, {"userID", m.id}} {
		props = append(props, userProperty(kv[0], kv[1])...)
	}
	var body []byte
	body = append(body, mqttString("MQTT")...)
	body = append(body, 5, 0x02) // version 5, clean start
	body = binary.BigEndian.AppendUint16(body, mqttKeepAlive)
	body = append(body, varint(len(props))...)
	body = append(body, props...)
	body = append(body, mqttString(fmt.Sprintf("%d%d", time.Now().UnixMilli(), 1000+rand.IntN(9000)))...)
	if err := m.send(ws, 0x10, body); err != nil {
		return "", err
	}
	typ, pkt, err := m.read(ws, mqttStepTimeout)
	if err != nil {
		return "", err
	}
	if pkt == nil {
		return "", transientErr("connack timed out")
	}
	if typ>>4 != 2 || len(pkt) < 2 {
		return "", fatalErr("expected connack, got %x", typ)
	}
	switch code := pkt[1]; code {
	case 0:
		return "", nil
	case 0x9c, 0x9d: // use another server, server moved
		p, _ := properties(pkt[2:])
		if p.serverRef == "" {
			return "", fatalErr("redirect without a server")
		}
		return p.serverRef, nil
	default:
		return "", fatalErr("connect refused: %#x", code)
	}
}

func (m *mqttLogin) subscribe(ws *websocket.Conn) error {
	var props []byte
	props = append(props, userProperty("authorization", "tmelogin")...)
	props = append(props, userProperty("pubsub", "unicast")...)
	body := binary.BigEndian.AppendUint16(nil, 1)
	body = append(body, varint(len(props))...)
	body = append(body, props...)
	body = append(body, mqttString("management.qrcode_login/"+m.id)...)
	body = append(body, 0) // QoS 0
	if err := m.send(ws, 0x82, body); err != nil {
		return err
	}
	for {
		typ, pkt, err := m.read(ws, mqttStepTimeout)
		if err != nil {
			return err
		}
		switch {
		case pkt == nil:
			return transientErr("suback timed out")
		case typ>>4 == 9 && len(pkt) >= 2 && binary.BigEndian.Uint16(pkt) == 1:
			_, n := properties(pkt[2:])
			for _, code := range pkt[2+n:] {
				if code >= 0x80 {
					return fatalErr("subscribe refused: %#x", code)
				}
			}
			return nil
		case typ>>4 == 3:
			if ev := publishEvent(typ, pkt); ev != nil && m.pending == nil {
				m.pending = ev
			}
		}
	}
}

// next waits up to wait for an event of the login, or returns nil.
func (m *mqttLogin) next(ws *websocket.Conn, wait time.Duration) (*mqttEvent, error) {
	until := time.Now().Add(wait)
	for {
		left := time.Until(until)
		if left <= 0 {
			return nil, nil
		}
		typ, pkt, err := m.read(ws, left)
		if err != nil || pkt == nil {
			return nil, err
		}
		switch typ >> 4 {
		case 3:
			if ev := publishEvent(typ, pkt); ev != nil {
				return ev, nil
			}
		case 12: // ping
			if err := m.send(ws, 0xd0, nil); err != nil {
				return nil, err
			}
		case 14: // disconnect
			return &mqttEvent{kind: mqttFailed}, nil
		}
	}
}

func (m *mqttLogin) send(ws *websocket.Conn, typ byte, body []byte) error {
	pkt := append([]byte{typ}, varint(len(body))...)
	pkt = append(pkt, body...)
	ws.SetWriteDeadline(time.Now().Add(mqttStepTimeout))
	if err := websocket.Message.Send(ws, pkt); err != nil {
		return transientErr("send failed: %v", err)
	}
	return nil
}

// read returns the next packet, its type byte and what follows its fixed
// header, or nil if none came within wait.
func (m *mqttLogin) read(ws *websocket.Conn, wait time.Duration) (byte, []byte, error) {
	for {
		if typ, pkt, n, ok := splitPacket(m.buf); ok {
			m.buf = m.buf[n:]
			return typ, pkt, nil
		}
		ws.SetReadDeadline(time.Now().Add(wait))
		var msg []byte
		if err := websocket.Message.Receive(ws, &msg); err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				return 0, nil, nil
			}
			return 0, nil, transientErr("read failed: %v", err)
		}
		m.buf = append(m.buf, msg...)
	}
}

// splitPacket takes the first whole packet off b.
func splitPacket(b []byte) (typ byte, pkt []byte, n int, ok bool) {
	if len(b) < 2 {
		return 0, nil, 0, false
	}
	length, size := 0, 0
	for i, mult := 1, 1; ; i, mult = i+1, mult*128 {
		if i >= len(b) || i > 4 {
			return 0, nil, 0, false
		}
		length += int(b[i]&0x7f) * mult
		if b[i]&0x80 == 0 {
			size = i + 1
			break
		}
	}
	if len(b) < size+length {
		return 0, nil, 0, false
	}
	return b[0], b[size : size+length], size + length, true
}

// publishEvent reads what a PUBLISH says of the login, from the type its
// user properties give it, or nil.
func publishEvent(typ byte, pkt []byte) *mqttEvent {
	if len(pkt) < 2 {
		return nil
	}
	n := int(binary.BigEndian.Uint16(pkt))
	at := 2 + n
	if (typ>>1)&3 > 0 {
		at += 2 // packet id
	}
	if at > len(pkt) {
		return nil
	}
	p, size := properties(pkt[at:])
	payload := pkt[min(at+size, len(pkt)):]
	switch p.user["type"] {
	case "scanned":
		return &mqttEvent{kind: mqttScanned}
	case "canceled":
		return &mqttEvent{kind: mqttCanceled}
	case "timeout":
		return &mqttEvent{kind: mqttExpired}
	case "loginFailed":
		return &mqttEvent{kind: mqttFailed}
	case "cookies":
		return cookiesEvent(payload)
	}
	return nil
}

// cookiesEvent reads the login's cookies, each a string, a number, or an
// object of its value.
func cookiesEvent(payload []byte) *mqttEvent {
	var p struct {
		Cookies map[string]json.RawMessage `json:"cookies"`
	}
	failed := &mqttEvent{kind: mqttFailed}
	if json.Unmarshal(payload, &p) != nil {
		return failed
	}
	value := func(name string) string {
		raw, ok := p.Cookies[name]
		if !ok {
			return ""
		}
		v, _ := decode(raw)
		if s := asString(v); s != "" {
			return s
		}
		return asString(get(v, "value"))
	}
	id := asInt(value("qqmusic_uin"))
	key := value("qqmusic_key")
	if id == 0 || key == "" {
		return failed
	}
	return &mqttEvent{kind: mqttCookies, musicID: id, musicKey: key}
}

type mqttProps struct {
	user      map[string]string
	serverRef string
}

// properties reads a property list, and returns it with the bytes it
// took, its length's included.
func properties(b []byte) (mqttProps, int) {
	p := mqttProps{user: map[string]string{}}
	length, size := uvarint(b)
	if size == 0 || size+length > len(b) {
		return p, len(b)
	}
	rest := b[size : size+length]
	readStr := func() string {
		if len(rest) < 2 {
			rest = nil
			return ""
		}
		n := int(binary.BigEndian.Uint16(rest))
		if 2+n > len(rest) {
			rest = nil
			return ""
		}
		s := string(rest[2 : 2+n])
		rest = rest[2+n:]
		return s
	}
	skip := func(n int) { rest = rest[min(n, len(rest)):] }
	for len(rest) > 0 {
		id := rest[0]
		rest = rest[1:]
		switch id {
		case 0x01, 0x17, 0x19, 0x24, 0x25, 0x28, 0x29, 0x2a:
			skip(1)
		case 0x13, 0x21, 0x22, 0x23:
			skip(2)
		case 0x02, 0x11, 0x18, 0x27:
			skip(4)
		case 0x0b:
			_, n := uvarint(rest)
			skip(max(n, 1))
		case 0x03, 0x08, 0x09, 0x12, 0x15, 0x16, 0x1a, 0x1f:
			readStr()
		case 0x1c:
			p.serverRef = readStr()
		case 0x26:
			k := readStr()
			p.user[k] = readStr()
		default:
			return p, size + length
		}
	}
	return p, size + length
}

func uvarint(b []byte) (int, int) {
	v := 0
	for i, mult := 0, 1; i < len(b) && i < 4; i, mult = i+1, mult*128 {
		v += int(b[i]&0x7f) * mult
		if b[i]&0x80 == 0 {
			return v, i + 1
		}
	}
	return 0, 0
}

func varint(n int) []byte {
	var out []byte
	for {
		b := byte(n % 128)
		n /= 128
		if n > 0 {
			b |= 0x80
		}
		out = append(out, b)
		if n == 0 {
			return out
		}
	}
}

func mqttString(s string) []byte {
	return append(binary.BigEndian.AppendUint16(nil, uint16(len(s))), s...)
}

func userProperty(k, v string) []byte {
	return append(append([]byte{0x26}, mqttString(k)...), mqttString(v)...)
}
