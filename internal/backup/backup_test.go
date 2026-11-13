package backup

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPathNamesTheBackupAfterTheTimestamp(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 30, 0, 0, time.UTC)

	if got, want := Path("/tmp/hs", now), "/tmp/hs.bak-20261113T103000"; got != want {
		t.Fatalf("Path = %q, want %q", got, want)
	}
}

// TestPathFormatsInUTC pins that the name does not move with the host zone: the
// gs-restore backup pattern this shares names every copy in UTC.
func TestPathFormatsInUTC(t *testing.T) {
	zone := time.FixedZone("UTC-5", -5*60*60)
	now := time.Date(2026, 11, 13, 5, 30, 0, 0, zone)

	if got, want := Path("/tmp/hs", now), "/tmp/hs.bak-20261113T103000"; got != want {
		t.Fatalf("Path = %q, want %q", got, want)
	}
}

func TestLatestPicksTheNewestBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hs")
	for _, name := range []string{
		"hs.bak-20260101T000000",
		"hs.bak-20260505T000000",
		"hs.bak-20261113T103000",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	if got, want := Latest(path), filepath.Join(dir, "hs.bak-20261113T103000"); got != want {
		t.Fatalf("Latest = %q, want %q", got, want)
	}
}

func TestLatestIsEmptyWithoutABackup(t *testing.T) {
	if got := Latest(filepath.Join(t.TempDir(), "hs")); got != "" {
		t.Fatalf("Latest = %q, want empty", got)
	}
}
