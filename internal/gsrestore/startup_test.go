package gsrestore

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDecideGateMatrix(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		want   Plan
	}{
		{name: "both off", config: Config{}, want: PlanNothing},
		{name: "auto without install is master-gated", config: Config{AutoInstall: true}, want: PlanNothing},
		{name: "install only hints", config: Config{Install: true}, want: PlanHint},
		{name: "install and auto install", config: Config{Install: true, AutoInstall: true}, want: PlanInstall},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Decide(test.config); got != test.want {
				t.Fatalf("Decide(%#v) = %q, want %q", test.config, got, test.want)
			}
		})
	}
}

func TestTakeHintOncePerSocket(t *testing.T) {
	root := t.TempDir()
	getenv := envFrom(map[string]string{
		"XDG_STATE_HOME":    root,
		"HERDR_SOCKET_PATH": "/tmp/herdr-one.sock",
	})
	now := time.Date(2026, 11, 12, 20, 0, 0, 0, time.UTC)

	first, err := TakeHint(getenv, now)
	if err != nil {
		t.Fatalf("TakeHint: %v", err)
	}
	if !first {
		t.Fatal("first TakeHint = false, want the hint this server run")
	}

	second, err := TakeHint(getenv, now)
	if err != nil {
		t.Fatalf("second TakeHint: %v", err)
	}
	if second {
		t.Fatal("second TakeHint = true, want it silent for the same socket")
	}

	other := envFrom(map[string]string{
		"XDG_STATE_HOME":    root,
		"HERDR_SOCKET_PATH": "/tmp/herdr-two.sock",
	})
	third, err := TakeHint(other, now)
	if err != nil {
		t.Fatalf("TakeHint for another socket: %v", err)
	}
	if !third {
		t.Fatal("TakeHint for another socket = false, want the hint once per socket")
	}
}

func TestHintPathFallsBackToTheStateDirectory(t *testing.T) {
	root := t.TempDir()
	getenv := envFrom(map[string]string{"XDG_STATE_HOME": root})
	path, err := HintPath(getenv)
	if err != nil {
		t.Fatalf("HintPath: %v", err)
	}
	want := filepath.Join(root, "herdr", "plugins", "herdr-suite", "startup-hint.json")
	if path != want {
		t.Fatalf("HintPath = %q, want %q", path, want)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stat %q = %v, want no state file until a hint is taken", path, err)
	}
}
