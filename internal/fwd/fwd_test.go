package fwd

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

func echoServer(t *testing.T, addr string) {
	t.Helper()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_, _ = io.Copy(c, c)
				c.Close()
			}()
		}
	}()
	t.Cleanup(func() { ln.Close() })
}

func TestServeProxy(t *testing.T) {
	back := freeAddr(t)
	echoServer(t, back)

	from := freeAddr(t)
	_, toPort, _ := net.SplitHostPort(back)
	_, fromPort, _ := net.SplitHostPort(from)
	cfg := Config{
		Bind: "127.0.0.1",
		Mappings: []Mapping{
			{From: atoi(t, fromPort), To: atoi(t, toPort)},
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, cfg) }()

	deadline := time.Now().Add(3 * time.Second)
	var c net.Conn
	for time.Now().Before(deadline) {
		var err error
		c, err = net.DialTimeout("tcp", from, time.Second)
		if err == nil {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if c == nil {
		t.Fatal("forward listener never came up")
	}
	defer c.Close()

	payload := []byte("ping-steam302-web")
	if _, err := c.Write(payload); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len(payload))
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatalf("echo read: %v", err)
	}
	if string(buf) != string(payload) {
		t.Fatalf("echo mismatch: %q", buf)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not stop after cancel")
	}
}

func atoi(t *testing.T, s string) int {
	t.Helper()
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRotateLog(t *testing.T) {
	dir := t.TempDir()
	logf := filepath.Join(dir, "fwd.log")

	for _, tc := range []struct {
		name string
		size int64
		max  int64
		want bool // 是否应发生轮转
	}{
		{"超过阈值轮转", 6 << 20, 5 << 20, true},
		{"等于阈值不轮转", 5 << 20, 5 << 20, false},
		{"远小于阈值不轮转", 1 << 10, 5 << 20, false},
		{"默认阈值轮转", 6 << 20, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(logf, make([]byte, tc.size), 0o644); err != nil {
				t.Fatal(err)
			}
			_ = os.Remove(logf + ".1")
			d := &Daemon{LogFile: logf, MaxLogBytes: tc.max}
			d.rotateLog()
			_, rotated := os.Stat(logf + ".1")
			if tc.want != (rotated == nil) {
				t.Fatalf("rotate=%v want=%v", rotated == nil, tc.want)
			}
			_ = os.Remove(logf)
		})
	}
}
