package fwd

import (
	"context"
	"fmt"
	"io"
	"net"
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