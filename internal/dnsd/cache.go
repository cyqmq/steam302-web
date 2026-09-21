package dnsd

import (
	"sync"
	"time"

	"github.com/miekg/dns"
)

type cachedMsg struct {
	Msg      *dns.Msg
	ExpireAt time.Time
}

func (c *cachedMsg) Expired() bool {
	return time.Now().After(c.ExpireAt)
}

// answerCache 是带最小 TTL 的上游应答缓存（简单 map + deadline）。
type answerCache struct {
	mu    sync.Mutex
	max   int
	items map[string]*cachedMsg
}

func newAnswerCache(max int) *answerCache {
	if max <= 0 {
		max = 256
	}
	return &answerCache{
		max:   max,
		items: make(map[string]*cachedMsg),
	}
}

func cacheKey(q *dns.Question) string {
	return q.Name + "|" + itoa(int(q.Qtype))
}

func (c *answerCache) Get(key string) (*cachedMsg, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	it, ok := c.items[key]
	if !ok {
		return nil, false
	}
	if it.Expired() {
		delete(c.items, key)
		return nil, false
	}
	return it, true
}

// Put 保存应答，有效期取应答实际 TTL 与最小 TTL 的较大者（防止短 TTl 抖了缓存）。
func (c *answerCache) Put(key string, m *dns.Msg, minTTL uint32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.items) >= c.max {
		// 简单清最旧一半
		n := 0
		for k := range c.items {
			delete(c.items, k)
			if n++; n >= c.max/2 {
				break
			}
		}
	}
	ttl := minTTL
	for _, rr := range m.Answer {
		if rr.Header().Ttl > ttl {
			ttl = rr.Header().Ttl
		}
	}
	c.items[key] = &cachedMsg{
		Msg:      m.Copy(),
		ExpireAt: time.Now().Add(time.Duration(ttl) * time.Second),
	}
}

const digits = "0123456789"

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = digits[v%10]
		v /= 10
	}
	return string(b[i:])
}
