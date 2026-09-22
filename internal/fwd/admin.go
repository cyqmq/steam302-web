package fwd

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// connInfo 描述一条活动的或刚结束的转发连接。
type connInfo struct {
	ID     uint64    `json:"id"`
	Remote string    `json:"remote"`
	Host   string    `json:"host"` // SNI 或 HTTP Host 嗅探结果（无法识别时为 ""）
	Up     int64     `json:"up"`   // 客户端→上游 字节
	Down   int64     `json:"down"` // 上游→客户端 字节
	Start  time.Time `json:"start"`
	Last   time.Time `json:"last"`
	Closed bool      `json:"closed"`
	End    time.Time `json:"end,omitempty"`
}

// connEvent 描述连接日志流里的一行结构化事件（连接建立/结束/失败）。
type connEvent struct {
	ID     uint64    `json:"id"`
	Kind   string    `json:"kind"` // accept | close | denied
	Level  string    `json:"level"`
	At     time.Time `json:"at"`
	Host   string    `json:"host"`
	Remote string    `json:"remote"`
	Up     int64     `json:"up"`
	Down   int64     `json:"down"`
	DurMs  int64     `json:"dur_ms"`
}

// tracker 维护活跃连接表与最近历史环形缓存。
type tracker struct {
	mu        sync.Mutex
	next      uint64
	live      map[uint64]net.Conn
	info      map[uint64]*connInfo
	recent    []connInfo
	events    []connEvent
	maxKeep   int
	started   time.Time
	totalUp   int64
	totalDown int64
}

func newTracker(maxKeep int) *tracker {
	if maxKeep <= 0 {
		maxKeep = 1000
	}
	return &tracker{
		live:    map[uint64]net.Conn{},
		info:    map[uint64]*connInfo{},
		maxKeep: maxKeep,
		started: time.Now(),
	}
}

// pushEvent 向连接事件环形缓存追加一条事件。
func (t *tracker) pushEvent(ev connEvent) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events = append(t.events, ev)
	if len(t.events) > t.maxKeep {
		t.events = t.events[len(t.events)-t.maxKeep:]
	}
}

// setEventHost 在嗅探识别出目标主机后，把对应连接的 accept 事件补上主机名。
func (t *tracker) setEventHost(id uint64, name string) {
	if name == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := len(t.events) - 1; i >= 0; i-- {
		if t.events[i].ID == id && t.events[i].Kind == "accept" {
			t.events[i].Host = name
			return
		}
	}
}

func (t *tracker) add(c net.Conn) (uint64, *connInfo) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.next++
	id := t.next
	inf := &connInfo{ID: id, Remote: c.RemoteAddr().String(), Start: time.Now(), Last: time.Now()}
	t.live[id] = c
	t.info[id] = inf
	host := ""
	t.events = append(t.events, connEvent{
		ID: id, Kind: "accept", Level: "INFO", At: inf.Start,
		Host: host, Remote: inf.Remote,
	})
	if len(t.events) > t.maxKeep {
		t.events = t.events[len(t.events)-t.maxKeep:]
	}
	return id, inf
}

func (t *tracker) setHost(name string, inf *connInfo) {
	if name == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	inf.Host = name
}

func (t *tracker) bump(inf *connInfo, up, down int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	inf.Up += up
	inf.Down += down
	inf.Last = time.Now()
	t.totalUp += up
	t.totalDown += down
}

func (t *tracker) remove(id uint64, c net.Conn) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if inf, ok := t.info[id]; ok {
		inf.Closed = true
		inf.End = time.Now()
		t.recent = append(t.recent, *inf)
		if len(t.recent) > t.maxKeep {
			t.recent = t.recent[len(t.recent)-t.maxKeep:]
		}
		t.events = append(t.events, connEvent{
			ID: id, Kind: "close", Level: "INFO", At: inf.End,
			Host: inf.Host, Remote: inf.Remote,
			Up: inf.Up, Down: inf.Down, DurMs: inf.End.Sub(inf.Start).Milliseconds(),
		})
		if len(t.events) > t.maxKeep {
			t.events = t.events[len(t.events)-t.maxKeep:]
		}
	}
	delete(t.live, id)
	delete(t.info, id)
}

func (t *tracker) closeOne(id uint64) bool {
	t.mu.Lock()
	c, ok := t.live[id]
	t.mu.Unlock()
	if !ok {
		return false
	}
	_ = c.Close()
	return true
}

func (t *tracker) closeAll() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := len(t.live)
	for _, c := range t.live {
		_ = c.Close()
	}
	return n
}

func (t *tracker) clearRecent() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.recent = t.recent[:0]
	t.events = t.events[:0]
}

func (t *tracker) snapshot() map[string]any {
	t.mu.Lock()
	defer t.mu.Unlock()
	live := make([]connInfo, 0, len(t.info))
	for _, inf := range t.info {
		live = append(live, *inf)
	}
	recent := make([]connInfo, len(t.recent))
	copy(recent, t.recent)
	return map[string]any{
		"start":      t.started,
		"active":     live,
		"active_n":   len(live),
		"recent":     recent,
		"total_up":   t.totalUp,
		"total_down": t.totalDown,
	}
}

func (t *tracker) eventsView() []connEvent {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]connEvent, len(t.events))
	copy(out, t.events)
	return out
}

// countingConn 转发两端套上计数。
type countingConn struct {
	net.Conn
	up, down int64
	notify   func(up, down int64)
	shutdown func()
	once     sync.Once
}

func (c *countingConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.down += int64(n)
	}
	return n, err
}

func (c *countingConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if n > 0 {
		c.up += int64(n)
	}
	return n, err
}

// finish 在连接结束时把累计字节数上报 tracker 并触发清理。
func (c *countingConn) finish() {
	c.once.Do(func() {
		c.notify(c.up, c.down)
		if c.shutdown != nil {
			c.shutdown()
		}
	})
}

// proxy 转发一条客户端连接：双向复制、字节计数、Host/SNI 嗅探、断连上报。
// 返回前会把该连接从活跃表移入最近历史。
func (t *tracker) proxy(c net.Conn, dst string) {
	br := bufio.NewReader(c)
	id, inf := t.add(c)
	sniffDone := make(chan struct{})
	go func() {
		defer close(sniffDone)
		if h := sniffHost(br, c); h != "" {
			t.setHost(h, inf)
			t.setEventHost(id, h)
		}
	}()
	up, err := net.DialTimeout("tcp", dst, 10*time.Second)
	if err != nil {
		<-sniffDone
		t.pushEvent(connEvent{ID: id, Kind: "denied", Level: "ERROR", At: time.Now(), Host: inf.Host, Remote: inf.Remote, Up: 0, Down: 0})
		t.remove(id, c)
		_ = c.Close()
		return
	}
	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		n, _ := io.Copy(up, br)
		if tcp, ok := up.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
		t.bump(inf, 0, n) // 客户端→上游
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		n, _ := io.Copy(c, up)
		if tcp, ok := c.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
		t.bump(inf, n, 0) // 上游→客户端
	}()
	<-done
	<-done
	t.remove(id, c)
	_ = c.Close()
	_ = up.Close()
}

// sniffHost 从最前面的字节里识别目标主机：优先 TLS SNI，其次 HTTP Host 头。
func sniffHost(br *bufio.Reader, conn net.Conn) string {
	if _, err := br.Peek(1); err != nil {
		return ""
	}
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		head, _ := br.Peek(8192)
		_ = tc.SetReadDeadline(time.Time{})
		return parseHost(head)
	}
	return ""
}

func parseHost(head []byte) string {
	if len(head) == 0 {
		return ""
	}
	if len(head) >= 5 && head[0] == 0x16 {
		// TLS record: type(1)=22 handshake, version(2), length(2)
		recordLen := int(binary.BigEndian.Uint16(head[3:5]))
		total := 5 + recordLen
		if total <= 8192 && total <= len(head) {
			if h := parseClientHelloSNI(head[5:total]); h != "" {
				return h
			}
		}
	}
	return parseHTTPHost(head)
}

// parseClientHelloSNI 解析 TLS ClientHello 握手消息体，返回 server_name。
func parseClientHelloSNI(b []byte) string {
	// handshake: type(1), length(3)
	if len(b) < 4 || b[0] != 1 {
		return ""
	}
	body := b[4:]
	if len(body) < 2 {
		return ""
	}
	// legacy_version(2), random(32), session_id[1+len]
	pos := 2 + 32
	if pos+1 > len(body) {
		return ""
	}
	sidLen := int(body[pos])
	pos += 1 + sidLen
	if pos+2 > len(body) {
		return ""
	}
	csLen := int(binary.BigEndian.Uint16(body[pos:]))
	pos += 2 + csLen
	if pos+1 > len(body) {
		return ""
	}
	cmLen := int(body[pos])
	pos += 1 + cmLen
	if pos+2 > len(body) {
		return ""
	}
	extLen := int(binary.BigEndian.Uint16(body[pos:]))
	pos += 2
	if pos+extLen > len(body) {
		return ""
	}
	ext := body[pos : pos+extLen]
	for len(ext) >= 4 {
		typ := binary.BigEndian.Uint16(ext)
		l := int(binary.BigEndian.Uint16(ext[2:4]))
		if len(ext) < 4+l {
			break
		}
		if typ == 0 { // server_name
			data := ext[4 : 4+l]
			if len(data) >= 2 {
				listLen := int(binary.BigEndian.Uint16(data))
				if listLen >= 3 && listLen <= len(data)-2 && data[2] == 0 { // name_type 0 = host_name
					nameLen := int(binary.BigEndian.Uint16(data[3:5]))
					if 5+nameLen <= len(data) {
						return string(data[5 : 5+nameLen])
					}
				}
			}
		}
		ext = ext[4+l:]
	}
	return ""
}

// parseHTTPHost 从明文开始（HTTP/1.x 或 CONNECT）里提取 Host。
func parseHTTPHost(head []byte) string {
	upper := strings.ToUpper(string(head))
	if !strings.HasPrefix(upper, "GET ") && !strings.HasPrefix(upper, "POST ") &&
		!strings.HasPrefix(upper, "HEAD ") && !strings.HasPrefix(upper, "PUT ") &&
		!strings.HasPrefix(upper, "CONNECT ") && !strings.HasPrefix(upper, "OPTIONS ") &&
		!strings.HasPrefix(upper, "PATCH ") && !strings.HasPrefix(upper, "DELETE ") {
		return ""
	}
	for _, line := range strings.Split(string(head), "\r\n") {
		if strings.HasPrefix(strings.ToLower(line), "host:") {
			h := strings.TrimSpace(line[5:])
			if h == "" {
				return ""
			}
			if host, _, err := net.SplitHostPort(h); err == nil {
				return host
			}
			return h
		}
	}
	return ""
}

// serveAdmin 启动本循环回环 JSON 管理接口。
func serveAdmin(addr string, tr *tracker) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /conns", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, tr.snapshot())
	})
	mux.HandleFunc("GET /conns/events", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"events": tr.eventsView()})
	})
	mux.HandleFunc("POST /conns/close", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ID uint64 `json:"id"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			writeJSON(w, map[string]string{"error": "bad body"})
			return
		}
		writeJSON(w, map[string]any{"closed": tr.closeOne(body.ID), "stats": tr.snapshot()})
	})
	mux.HandleFunc("POST /conns/close-all", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"closed": tr.closeAll(), "stats": tr.snapshot()})
	})
	mux.HandleFunc("POST /conns/clear", func(w http.ResponseWriter, r *http.Request) {
		tr.clearRecent()
		writeJSON(w, map[string]any{"ok": true})
	})
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return
	}
	_ = http.Serve(ln, mux)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	_ = enc.Encode(v)
}
