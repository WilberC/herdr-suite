package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestPluginBuildHookWiresEnsureBinary(t *testing.T) {
	manifest, err := os.ReadFile("../../herdr-plugin.toml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifest), `command = ["bash", "bin/ensure-binary.sh", "--in-tree"]`) {
		t.Fatal("herdr-plugin.toml has no [[build]] hook invoking bin/ensure-binary.sh --in-tree; plugin installs cannot provision bin/hs")
	}

	script, err := os.ReadFile("../../bin/ensure-binary.sh")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(script), `REPO="WilberC/herdr-suite"`) {
		t.Fatal("bin/ensure-binary.sh must download from WilberC/herdr-suite")
	}
	if !strings.Contains(string(script), `"${osName}-${archName}"`) && !strings.Contains(string(script), `hs-`) {
		t.Fatal("bin/ensure-binary.sh must name release assets after the hs-<os>-<arch> matrix")
	}
	if !strings.Contains(string(script), "./cmd/hs") {
		t.Fatal("bin/ensure-binary.sh must build ./cmd/hs")
	}

	info, err := os.Stat("../../bin/ensure-binary.sh")
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatal("bin/ensure-binary.sh is not executable")
	}
	if bash, err := exec.LookPath("bash"); err == nil {
		if out, err := exec.Command(bash, "-n", "../../bin/ensure-binary.sh").CombinedOutput(); err != nil {
			t.Fatalf("bin/ensure-binary.sh has a syntax error: %v: %s", err, out)
		}
	}
}
