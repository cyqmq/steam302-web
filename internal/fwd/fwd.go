// Package fwd implements a userspace TCP port-forward proxy used to expose the
// non-privileged Caddy HTTPS/HTTP listeners (default 25584/24196) on the public
// ports browsers actually use (443/80). It mirrors what the original
// Steamcommunity_302 tool does (forward 127.0.0.1:443 -> :30081) without
// requiring iptables/nft administration.
package fwd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Mapping forwards a From port to a To port on the bind address.
type Mapping struct {
	From int `json:"from"`
	To   int `json:"to"`
}

// Config describes the forward set. PidFile/LogFile are managed by the CLI.
type Config struct {
	Bind     string    `json:"bind"`
	PidFile  string    `json:"pid_file"`
	LogFile  string    `json:"log_file"`
	Mappings []Mapping `json:"mappings"`
}

// IsZero reports whether the config has no mappings to run.
func (c Config) IsZero() bool {
	return len(c.Mappings) == 0
}

// BindParam returns the listen address for one mapping.
func (c Config) BindParam(m Mapping) string {
	return net.JoinHostPort(c.Bind, strconv.Itoa(m.From))
}

// Serve runs all forwarding listeners until ctx is cancelled or a listener
// fails to bind; the first bind error is returned.
func Serve(ctx context.Context, cfg Config) error {
	if cfg.Bind == "" {
		cfg.Bind = "127.0.0.1"
	}
	if cfg.IsZero() {
		return errors.New("fwd: no forwarding mappings configured")
	}
	var wg sync.WaitGroup
	errCh := make(chan error, len(cfg.Mappings))
	for _, m := range cfg.Mappings {
		wg.Add(1)
		go func(m Mapping) {
			defer wg.Done()
			if err := serveOne(ctx, cfg, m); err != nil {
				select {
				case errCh <- fmt.Errorf("%s -> %d: %w", cfg.BindParam(m), m.To, err):
				default:
				}
			}
		}(m)
	}
	wg.Wait()
	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

func serveOne(ctx context.Context, cfg Config, m Mapping) error {
	ln, err := net.Listen("tcp", cfg.BindParam(m))
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			return err
		}
		go proxy(c, net.JoinHostPort(cfg.Bind, strconv.Itoa(m.To)))
	}
}

// proxy pipes data in both directions and closes both ends when either side
// finishes.
func proxy(c net.Conn, dst string) {
	defer c.Close()
	up, err := net.DialTimeout("tcp", dst, 10*time.Second)
	if err != nil {
		return
	}
	defer up.Close()
	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		_, _ = io.Copy(up, c)
		if tcp, ok := up.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		_, _ = io.Copy(c, up)
		if tcp, ok := c.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
	}()
	<-done
	<-done
}

// Daemon manages the detached run process through a pid file.
type Daemon struct {
	Bind    string
	PidFile string
	LogFile string
	WorkDir string
}

// Up starts the foreground listener in a detached child (self re-exec) and
// records its pid. True is returned when the daemon was already running.
func (d *Daemon) Up() (bool, error) {
	if pid, err := d.pid(); err == nil {
		if alive(pid) {
			return true, nil
		}
		_ = os.Remove(d.PidFile)
	}
	exe, err := os.Executable()
	if err != nil {
		return false, err
	}
	logf := d.LogFile
	var logfh *os.File
	if logf != "" {
		logfh, err = os.OpenFile(logf, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return false, err
		}
	} else {
		logfh, err = os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			return false, err
		}
	}
	cmd := exec.Command(exe, "run")
	cmd.Stdout = logfh
	cmd.Stderr = logfh
	cmd.Dir = d.WorkDir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return false, err
	}
	if err := os.WriteFile(d.PidFile, []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o644); err != nil {
		_ = cmd.Process.Kill()
		return false, err
	}
	return false, nil
}

// Down stops a running daemon and removes the pid file.
func (d *Daemon) Down() (bool, error) {
	pid, err := d.pid()
	if err != nil {
		return false, nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		_ = os.Remove(d.PidFile)
		return false, nil
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		_ = os.Remove(d.PidFile)
		return false, nil
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !alive(pid) {
			_ = os.Remove(d.PidFile)
			return true, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return true, errors.New("fwd: process did not exit within 5s")
}

// Status reports whether the daemon pid file points at a live process.
func (d *Daemon) Status() (pid int, running bool) {
	pid, err := d.pid()
	if err != nil {
		return 0, false
	}
	return pid, alive(pid)
}

func (d *Daemon) pid() (int, error) {
	b, err := os.ReadFile(d.PidFile)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0, err
	}
	return pid, nil
}

func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}
