package pathshim

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/WilberC/herdr-suite/internal/backup"
)

// fileMode is the shim's mode: a shell has to be able to run it.
const fileMode = 0o755

// InstallResult is what an install did, so the caller can report exactly that.
type InstallResult struct {
	Dir     string
	Path    string
	Backup  string
	Wrote   bool
	Already bool
}

// Install writes the shim into dir, creating the directory when it is missing.
// An identical shim is left alone. A different hs is refused unless force is
// set, and then it is backed up first; a repeated forced install refreshes that
// one backup instead of accumulating copies.
func Install(dir string, force bool, now time.Time) (InstallResult, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return InstallResult{}, fmt.Errorf("create %s: %w", dir, err)
	}
	path := filepath.Join(dir, FileName)
	result := InstallResult{Dir: dir, Path: path}

	existing, err := os.ReadFile(path)
	switch {
	case err == nil:
		if bytes.Equal(existing, []byte(Script)) {
			result.Already = true
			return result, nil
		}
		if !force {
			return InstallResult{}, Refusal{Reason: fmt.Sprintf(
				"%s already exists and is not an hs path shim; pass --force to replace it and keep a backup", path)}
		}
		backupPath := backup.Latest(path)
		if backupPath == "" {
			backupPath = backup.Path(path, now)
		}
		if err := os.WriteFile(backupPath, existing, 0o644); err != nil {
			return InstallResult{}, fmt.Errorf("back up %s: %w", path, err)
		}
		result.Backup = backupPath
	case os.IsNotExist(err):
	default:
		return InstallResult{}, fmt.Errorf("read %s: %w", path, err)
	}

	if err := os.WriteFile(path, []byte(Script), fileMode); err != nil {
		return InstallResult{}, fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Chmod(path, fileMode); err != nil {
		return InstallResult{}, fmt.Errorf("make %s executable: %w", path, err)
	}
	result.Wrote = true
	return result, nil
}

// RemoveResult is what a remove did.
type RemoveResult struct {
	Path    string
	Backup  string
	Removed bool
	Missing bool
}

// Remove deletes the shim this command writes. A file it did not write is
// refused unless a backup of it already exists or force is set, which backs it
// up first. Backups are never deleted.
func Remove(dir string, force bool, now time.Time) (RemoveResult, error) {
	path := filepath.Join(dir, FileName)
	result := RemoveResult{Path: path}

	existing, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		result.Missing = true
		return result, nil
	case err != nil:
		return RemoveResult{}, fmt.Errorf("read %s: %w", path, err)
	}

	ours := bytes.Equal(existing, []byte(Script))
	backupPath := backup.Latest(path)
	if !ours && backupPath == "" {
		if !force {
			return RemoveResult{}, Refusal{Reason: fmt.Sprintf(
				"%s is not an hs path shim; pass --force to remove it and keep a backup", path)}
		}
		backupPath = backup.Path(path, now)
		if err := os.WriteFile(backupPath, existing, 0o644); err != nil {
			return RemoveResult{}, fmt.Errorf("back up %s: %w", path, err)
		}
	}
	if err := os.Remove(path); err != nil {
		return RemoveResult{}, fmt.Errorf("remove %s: %w", path, err)
	}
	result.Removed = true
	result.Backup = backupPath
	return result, nil
}
