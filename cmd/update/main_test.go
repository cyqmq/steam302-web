package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDifferentVersion(t *testing.T) {
	cases := []struct {
		cur, latest string
		want        bool
	}{
		{"2.0.0", "2.0.0", false},
		{"2.0.0", "2.0.1", true},
		{"2.1.0", "2.0.9", false},
		{"v2.0.0", "2.0.1", true},
		{"2.0.0", "10.0.0", true},
		{"3.0.0", "2.9.9", false},
	}
	for _, c := range cases {
		if got := differentVersion(c.cur, c.latest); got != c.want {
			t.Errorf("differentVersion(%q,%q)=%v want %v", c.cur, c.latest, got, c.want)
		}
	}
}

func TestReadSumAndVerify(t *testing.T) {
	dir := t.TempDir()
	data := []byte("hello update world")
	sum := sha256.Sum256(data)
	sumFile := filepath.Join(dir, "x.tar.gz.sha256")
	os.WriteFile(sumFile, []byte(hex.EncodeToString(sum[:])+"  x.tar.gz\n"), 0o644)
	tarFile := filepath.Join(dir, "x.tar.gz")
	os.WriteFile(tarFile, data, 0o644)

	got, err := readSum(sumFile)
	if err != nil {
		t.Fatal(err)
	}
	if got != hex.EncodeToString(sum[:]) {
		t.Fatalf("readSum mismatch")
	}
	if err := verifySHA256(tarFile, got); err != nil {
		t.Fatal(err)
	}
	if err := verifySHA256(tarFile, strings.Repeat("0", 64)); err == nil {
		t.Fatal("期望校验失败")
	}
}

// makeTarball 生成含 bin/ 与 deploy/ 的发布包。
func makeTarball(t *testing.T, root string, files map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rel.tar.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		hdr := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(content))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()
	return path
}

func TestSwapReplacesAndRollsBack(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	os.MkdirAll(binDir, 0o755)
	os.WriteFile(filepath.Join(binDir, "webui"), []byte("OLD-WEBUI"), 0o755)
	os.WriteFile(filepath.Join(binDir, "keep"), []byte("OLD-KEEP"), 0o755)

	top := filepath.Dir(root)
	staging = filepath.Join(top, "staging-update-test")
	tarPath := makeTarball(t, t.TempDir(), map[string]string{
		"bin/webui":         "NEW-WEBUI",
		"bin/newtool":       "NEW-TOOL",
		"deploy/install.sh": "NEW-INSTALL",
		"config/skipme":     "SHOULD-NOT-COPY",
	})
	if err := swap(root, tarPath, filepath.Join(staging, "backup-000")); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(binDir, "webui"), "NEW-WEBUI")
	assertFile(t, filepath.Join(binDir, "keep"), "OLD-KEEP")
	assertFile(t, filepath.Join(binDir, "newtool"), "NEW-TOOL")
	assertFile(t, filepath.Join(root, "deploy", "install.sh"), "NEW-INSTALL")
	if _, err := os.Stat(filepath.Join(root, "config", "skipme")); err == nil {
		t.Fatal("不应复制 config/ 下的文件")
	}
}

func TestSwapRollbackOnCorrupt(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	os.MkdirAll(binDir, 0o755)
	os.WriteFile(filepath.Join(binDir, "webui"), []byte("OLD-WEBUI"), 0o755)

	// 造一个损坏的 tar：swap 应失败且不破坏现有文件。
	bad := filepath.Join(t.TempDir(), "bad.tar.gz")
	os.WriteFile(bad, []byte("not a real gzip, but long enough to fail late"), 0o644)

	top := filepath.Dir(root)
	staging = filepath.Join(top, "staging-update-test2")
	if err := swap(root, bad, filepath.Join(staging, "backup-000")); err == nil {
		t.Fatal("期望损坏包导致失败")
	}
	assertFile(t, filepath.Join(binDir, "webui"), "OLD-WEBUI")
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s: %v", path, err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", path, data, want)
	}
}
