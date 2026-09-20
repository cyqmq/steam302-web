package hosts

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Split divides content into lines that do NOT carry marker and those that do.
func Split(content, marker string) (foreign, ours []string) {
	for _, line := range strings.Split(content, "\n") {
		if strings.Contains(line, marker) {
			ours = append(ours, line)
		} else {
			foreign = append(foreign, line)
		}
	}
	return foreign, ours
}

// Merge removes any previously applied marker lines and appends block at the end.
func Merge(content, marker, block string) string {
	foreign, _ := Split(content, marker)
	lines := foreign
	if block != "" {
		b := strings.TrimRight(block, "\n")
		lines = trimTrailingBlank(lines)
		lines = append(lines, b)
	}
	return strings.Join(lines, "\n") + "\n"
}

func trimTrailingBlank(lines []string) []string {
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// Read reads a host file (defaults to /etc/hosts).
func Read(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// MustApply writes path with merged content atomically. Returns new content.
func MustApply(path, marker, block string, dryRun bool) (string, error) {
	cur, err := Read(path)
	if err != nil {
		return "", err
	}
	next := Merge(cur, marker, block)
	if dryRun {
		return next, nil
	}
	return next, writeAtomic(path, next)
}

// MustRemove strips marker lines from path atomically.
func MustRemove(path, marker string, dryRun bool) (string, error) {
	cur, err := Read(path)
	if err != nil {
		return "", err
	}
	foreign, _ := Split(cur, marker)
	next := strings.Join(trimTrailingBlank(foreign), "\n") + "\n"
	if dryRun {
		return next, nil
	}
	return next, writeAtomic(path, next)
}

// BackupDir stores the pristine hosts file under dir before any modification.
func Backup(path, backupDir string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return err
	}
	bak := filepath.Join(backupDir, "hosts.bak")
	if _, err := os.Stat(bak); err == nil {
		return nil // already backed up
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return os.WriteFile(bak, data, 0o644)
}

// Revert restores hosts.bak from backupDir onto path.
func Revert(path, backupDir string) error {
	bak := filepath.Join(backupDir, "hosts.bak")
	data, err := os.ReadFile(bak)
	if err != nil {
		return err
	}
	return writeAtomic(path, string(data))
}

func writeAtomic(path, content string) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".hosts-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename %s: %w", path, err)
	}
	return nil
}
