package fwd

import (
	"bytes"
	"testing"
)

func TestParseClientHelloSNI(t *testing.T) {
	// 用真实 TLS ClientHello 形状：先通过 net 建立，简化——直接构造最小合法结构：
	// 手写一个足 Min 的 ClientHello（含 server_name 扩展）。
	ch := buildHello(t, "github.com")
	// 拼上握手头: type(1)=1 + 3 字节长度，parseClientHelloSNI 从 b[4:] 起解析。
	msg := []byte{0x01, byte(len(ch) >> 16), byte(len(ch) >> 8), byte(len(ch))}
	msg = append(msg, ch...)
	got := parseClientHelloSNI(msg)
	if got != "github.com" {
		t.Fatalf("SNI = %q, want github.com", got)
	}
}

// buildHello 手工构造一个包含 server_name 扩展的 TLS 1.3 ClientHello 握手体。
func buildHello(t *testing.T, name string) []byte {
	t.Helper()
	if len(name) > 255 {
		t.Fatal("name too long")
	}
	var b bytes.Buffer
	b.Write([]byte{0x03, 0x03}) // legacy_version
	b.Write(make([]byte, 32))   // random
	b.Write([]byte{0x00})       // session_id len
	b.Write([]byte{0x00, 0x02}) // cipher_suites len
	b.Write([]byte{0x13, 0x01}) // TLS_AES_128_GCM_SHA256
	b.Write([]byte{0x01, 0x00}) // compression_methods

	// server_name(0) 扩展：type(2) + data_len(2) + data(ServerNameList)
	list := bytes.Buffer{}
	list.Write([]byte{0x00, byte(len(name)) + 3}) // name list len（1 type + 2 len 头）
	list.Write([]byte{0x00})                      // name_type host_name
	list.Write([]byte{0x00, byte(len(name))})     // name len
	list.Write([]byte(name))                      // value
	ext := bytes.Buffer{}
	ext.Write([]byte{0x00, 0x00})             // ext type = server_name(0)
	ext.Write([]byte{0x00, byte(list.Len())}) // ext data len
	ext.Write(list.Bytes())
	extData := ext.Bytes()
	b.Write([]byte{0x00, byte(len(extData))}) // extensions len
	b.Write(extData)
	return b.Bytes()
}

func TestParseClientHelloSNIReal(t *testing.T) {
	// 直接打真实 TLS 服务？不行，无网络依赖。改用两条常见情况：
	for _, tc := range [][]byte{
		{0x01, 0x00, 0x00, 0x00},       // 长度不足
		{0x02, 0x00, 0x00, 0x03, 0x00}, // 类型不是 ClientHello
	} {
		if got := parseClientHelloSNI(tc); got != "" {
			t.Fatalf("malformed input returned %q", got)
		}
	}
}

func TestParseHTTPHost(t *testing.T) {
	head := []byte("CONNECT forwarded.github.com:443 HTTP/1.1\r\nHost: forwarded.github.com:443\r\n")
	if got := parseHTTPHost(head); got != "forwarded.github.com" {
		t.Fatalf("host = %q", got)
	}
	head2 := []byte("GET / HTTP/1.1\r\nhost: store.steampowered.com\r\n")
	if got := parseHTTPHost(head2); got != "store.steampowered.com" {
		t.Fatalf("host = %q", got)
	}
	if got := parseHTTPHost([]byte("PRI * HTTP/2.0\r\n")); got != "" {
		t.Fatalf("h2 preface should not match, got %q", got)
	}
}
