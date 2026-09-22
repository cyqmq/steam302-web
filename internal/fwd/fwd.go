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
	Bind        string    `json:"bind"`
	PidFile     string    `json:"pid_file"`
	LogFile     string    `json:"log_file"`
	LogMaxBytes int64     `json:"log_max_bytes"`
	Mappings    []Mapping `json:"mappings"`
	AdminAddr   string    `json:"admin_addr,omitempty"` // 回环连接监控端口（如 127.0.0.1:28001）
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
// fails to bind; the first bind error is returned. When cfg.AdminAddr is set,
// a loopback JSON API (GET /conns 等) 一并启动，供外部读取连接监控。
func Serve(ctx context.Context, cfg Config) error {
	if cfg.Bind == "" {
		cfg.Bind = "127.0.0.1"
	}
	if cfg.IsZero() {
		return errors.New("fwd: no forwarding mappings configured")
	}
	tr := newTracker(1000)
	if cfg.AdminAddr != "" {
		go serveAdmin(cfg.AdminAddr, tr)
	}
	var wg sync.WaitGroup
	errCh := make(chan error, len(cfg.Mappings))
	for _, m := range cfg.Mappings {
		wg.Add(1)
		go func(m Mapping) {
			defer wg.Done()
			if err := serveOne(ctx, cfg, m, tr); err != nil {
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

func serveOne(ctx context.Context, cfg Config, m Mapping, tr *tracker) error {
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
		go tr.proxy(c, net.JoinHostPort(cfg.Bind, strconv.Itoa(m.To)))
	}
}

// Daemon manages the detached run process through a pid file.
type Daemon struct {
	Bind        string
	PidFile     string
	LogFile     string
	MaxLogBytes int64 // 日志轮转阈值；<=0 使用默认 5MB
	WorkDir     string
}

// defaultMaxLogBytes is the size (in bytes) at which the daemon log rotates.
const defaultMaxLogBytes = 5 << 20

// rotateLog renames logf to logf+".1" when it exceeds max (best effort).
func (d *Daemon) rotateLog() {
	if d.LogFile == "" {
		return
	}
	max := d.MaxLogBytes
	if max <= 0 {
		max = defaultMaxLogBytes
	}
	info, err := os.Stat(d.LogFile)
	if err != nil || info.Size() <= max {
		return
	}
	_ = os.Rename(d.LogFile, d.LogFile+".1")
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
		d.rotateLog()
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
