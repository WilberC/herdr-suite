package gsrestore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveTargetRefusesAnUnsetAgentDir(t *testing.T) {
	_, err := ResolveTarget(envFrom(map[string]string{}))
	if err == nil {
		t.Fatal("ResolveTarget accepted a run without PI_CODING_AGENT_DIR")
	}
	if !strings.Contains(err.Error(), "PI_CODING_AGENT_DIR") {
		t.Fatalf("error = %q, want it to name PI_CODING_AGENT_DIR", err)
	}
	var refusal Refusal
	if !asRefusal(err, &refusal) {
		t.Fatalf("error = %T, want a Refusal", err)
	}
}

func TestResolveTargetRefusesAnAgentDirWithoutExtensions(t *testing.T) {
	root := t.TempDir()
	agentDir := filepath.Join(root, "agent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := ResolveTarget(envFrom(map[string]string{"PI_CODING_AGENT_DIR": agentDir}))
	if err == nil {
		t.Fatal("ResolveTarget accepted an agent dir with no extensions/ directory")
	}
	extensions := filepath.Join(agentDir, "extensions")
	if !strings.Contains(err.Error(), extensions) {
		t.Fatalf("error = %q, want it to name %q", err, extensions)
	}
	if !strings.Contains(err.Error(), "mkdir -p") {
		t.Fatalf("error = %q, want it to suggest creating the directory", err)
	}
}

func TestResolveTargetNamesTheShippedFile(t *testing.T) {
	root := t.TempDir()
	agentDir := filepath.Join(root, "agent")
	extensions := filepath.Join(agentDir, "extensions")
	if err := os.MkdirAll(extensions, 0o755); err != nil {
		t.Fatal(err)
	}

	target, err := ResolveTarget(envFrom(map[string]string{"PI_CODING_AGENT_DIR": agentDir}))
	if err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}
	if target.AgentDir != agentDir {
		t.Fatalf("AgentDir = %q, want %q", target.AgentDir, agentDir)
	}
	if target.ExtensionsDir != extensions {
		t.Fatalf("ExtensionsDir = %q, want %q", target.ExtensionsDir, extensions)
	}
	if want := filepath.Join(extensions, ReporterFileName); target.Path != want {
		t.Fatalf("Path = %q, want %q", target.Path, want)
	}
}

func TestResolveTargetRefusesARelativeAgentDir(t *testing.T) {
	_, err := ResolveTarget(envFrom(map[string]string{"PI_CODING_AGENT_DIR": "relative/agent"}))
	if err == nil {
		t.Fatal("ResolveTarget accepted a relative PI_CODING_AGENT_DIR")
	}
	if !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("error = %q, want it to ask for an absolute path", err)
	}
}

// asRefusal is errors.As specialized to Refusal, kept here so the test reads
// without importing errors in every case.
func asRefusal(err error, target *Refusal) bool {
	for err != nil {
		if refusal, ok := err.(Refusal); ok {
			*target = refusal
			return true
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapper.Unwrap()
	}
	return false
}
