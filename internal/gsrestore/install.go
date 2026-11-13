package gsrestore

import (
	"bytes"
	"fmt"
	"os"
	"time"

	"github.com/WilberC/herdr-suite/internal/backup"
)

// InstallResult is what an install did, so the caller can report it or stay
// silent.
type InstallResult struct {
	Path    string
	Backup  string
	Wrote   bool
	Already bool
}

// Install copies reporter into target. An identical file is left alone. A file
// that is a herdr reporter is backed up first; a file that is someone else's is
// refused unless force is set. Repeat installs keep a single backup: the
// existing one is overwritten instead of accumulating.
func Install(target Target, reporter []byte, force bool, now time.Time) (InstallResult, error) {
	if _, ok := markerVersion(reporter); !ok {
		return InstallResult{}, fmt.Errorf("the reporter source has no %s marker; refusing to install it", "HERDR_SUITE_GS_RESTORE_VERSION")
	}

	existing, err := os.ReadFile(target.Path)
	switch {
	case err == nil:
		if bytes.Equal(existing, reporter) {
			return InstallResult{Path: target.Path, Already: true}, nil
		}
		if !looksLikeReporter(existing) && !force {
			return InstallResult{}, Refusal{Reason: fmt.Sprintf(
				"%s already exists and is not a herdr agent reporter; pass --force to replace it and keep a backup", target.Path)}
		}
		backupPath := LatestBackup(target.Path)
		if backupPath == "" {
			backupPath = backup.Path(target.Path, now)
		}
		if err := os.WriteFile(backupPath, existing, 0o644); err != nil {
			return InstallResult{}, fmt.Errorf("back up %s: %w", target.Path, err)
		}
		if err := os.WriteFile(target.Path, reporter, 0o644); err != nil {
			return InstallResult{}, fmt.Errorf("write %s: %w", target.Path, err)
		}
		return InstallResult{Path: target.Path, Backup: backupPath, Wrote: true}, nil
	case os.IsNotExist(err):
		if err := os.WriteFile(target.Path, reporter, 0o644); err != nil {
			return InstallResult{}, fmt.Errorf("write %s: %w", target.Path, err)
		}
		return InstallResult{Path: target.Path, Wrote: true}, nil
	default:
		return InstallResult{}, fmt.Errorf("read %s: %w", target.Path, err)
	}
}

// RemoveResult is what a remove did.
type RemoveResult struct {
	Path     string
	Backup   string
	Restored bool
	Removed  bool
}

// Remove puts the latest backup back, or deletes the reporter when no backup
// exists. It never touches an unrelated file and never deletes the backup.
func Remove(target Target) (RemoveResult, error) {
	result := RemoveResult{Path: target.Path}
	if backup := LatestBackup(target.Path); backup != "" {
		data, err := os.ReadFile(backup)
		if err != nil {
			return RemoveResult{}, fmt.Errorf("read the backup %s: %w", backup, err)
		}
		if err := os.WriteFile(target.Path, data, 0o644); err != nil {
			return RemoveResult{}, fmt.Errorf("restore %s: %w", target.Path, err)
		}
		result.Backup = backup
		result.Restored = true
		return result, nil
	}

	if err := os.Remove(target.Path); err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return RemoveResult{}, fmt.Errorf("remove %s: %w", target.Path, err)
	}
	result.Removed = true
	return result, nil
}

// LatestBackup is the backup Remove restores and Install overwrites: the
// greatest name, which the timestamp suffix makes the most recent.
func LatestBackup(path string) string {
	return backup.Latest(path)
}
