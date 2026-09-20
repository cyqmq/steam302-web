package hosts

import (
	"os"
	"strings"
	"testing"
)

const marker = "#S302X"

func TestMerge(t *testing.T) {
	cur := "127.0.0.1 localhost\n" +
		"127.0.0.1 github.com " + marker + "\n" +
		"\n"
	block := "127.0.0.1 github.com " + marker + "\n" +
		"127.0.0.1 api.github.com " + marker + "\n"
	got := Merge(cur, marker, block)
	want := "127.0.0.1 localhost\n" +
		"127.0.0.1 github.com " + marker + "\n" +
		"127.0.0.1 api.github.com " + marker + "\n"
	if got != want {
		t.Errorf("Merge mismatch:\n got=%q\nwant=%q", got, want)
	}
}

func TestMergeFromClean(t *testing.T) {
	cur := "127.0.0.1 localhost\n"
	got := Merge(cur, marker, "a.com "+marker)
	want := "127.0.0.1 localhost\n" + "a.com " + marker + "\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMustRemove(t *testing.T) {
	path := t.TempDir() + "/hosts"
	cur := "127.0.0.1 localhost\n" +
		"127.0.0.1 github.com " + marker + "\n" +
		"127.0.0.1 api.github.com " + marker + "\n"
	if err := writeAtomic(path, cur); err != nil {
		t.Fatal(err)
	}
	next, err := MustRemove(path, marker, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(next, marker) {
		t.Errorf("marker still present: %q", next)
	}
	if next != "127.0.0.1 localhost\n" {
		t.Errorf("got %q", next)
	}
}

func TestBackupRevert(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/hosts"
	bak := dir + "/backup"
	if err := writeAtomic(path, "127.0.0.1 localhost\n"); err != nil {
		t.Fatal(err)
	}
	if err := Backup(path, bak); err != nil {
		t.Fatal(err)
	}
	if _, err := MustApply(path, marker, "a.com "+marker+"\n", false); err != nil {
		t.Fatal(err)
	}
	if err := Revert(path, bak); err != nil {
		t.Fatal(err)
	}
	got, _ := Read(path)
	if got != "127.0.0.1 localhost\n" {
		t.Errorf("revert mismatch: %q", got)
	}
}

func TestSnapshotPrune(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/hosts"
	bak := dir + "/backup"
	if err := writeAtomic(path, "127.0.0.1 localhost\n"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		if err := Snapshot(path, bak, 3); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(bak)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "hosts.bak.") {
			n++
		}
	}
	if n != 3 {
		t.Fatalf("expected 3 snapshots kept, got %d", n)
	}
}
