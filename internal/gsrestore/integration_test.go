package gsrestore

import (
	"strings"
	"testing"
)

func TestParseIntegrationVersionReadsEveryStatusShape(t *testing.T) {
	tests := []struct {
		name string
		text string
		want int
		ok   bool
	}{
		{
			name: "outdated version",
			text: "pi: outdated (v9 < v10) (/home/user/.config/herdr/extensions/herdr-agent-state.ts)\n",
			want: 10,
			ok:   true,
		},
		{
			name: "outdated legacy",
			text: "pi: outdated (legacy < v10) (/tmp/extensions/herdr-agent-state.ts)\n",
			want: 10,
			ok:   true,
		},
		{
			name: "current",
			text: "pi: current (v9) (/tmp/extensions/herdr-agent-state.ts)\n",
			want: 9,
			ok:   true,
		},
		{
			name: "only the pi line counts",
			text: "claude: current (v11) (/tmp/claude)\npi: current (v12) (/tmp/pi)\nomp: not installed (/tmp/omp)\n",
			want: 12,
			ok:   true,
		},
		{
			name: "not installed names no version",
			text: "pi: not installed (/tmp/extensions/herdr-agent-state.ts)\n",
			ok:   false,
		},
		{
			name: "needs repair names the installed version, not the expected one",
			text: "pi: needs repair (v10) (/tmp/extensions/herdr-agent-state.ts)\n",
			ok:   false,
		},
		{
			name: "garbage",
			text: "herdr: no integration information here\n",
			ok:   false,
		},
		{
			name: "empty",
			text: "",
			ok:   false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			version, ok := ParseIntegrationVersion(test.text)
			if ok != test.ok || version != test.want {
				t.Fatalf("ParseIntegrationVersion(%q) = %d, %v, want %d, %v", test.text, version, ok, test.want, test.ok)
			}
		})
	}
}

func TestIntegrationVersionFallsBackToTheShippedDefault(t *testing.T) {
	if ShippedIntegrationVersion != 9 {
		t.Fatalf("ShippedIntegrationVersion = %d, want 9, the pi integration herdr 0.9.5 expects", ShippedIntegrationVersion)
	}

	tests := []struct {
		name      string
		text      string
		readable  bool
		version   int
		fromHerdr bool
	}{
		{name: "herdr answered", text: "pi: current (v10) (/tmp/pi)\n", readable: true, version: 10, fromHerdr: true},
		{name: "herdr is behind the file", text: "pi: outdated (v12 < v11) (/tmp/pi)\n", readable: true, version: 11, fromHerdr: true},
		{name: "herdr could not be run", readable: false, version: ShippedIntegrationVersion, fromHerdr: false},
		{name: "herdr refused", text: "pi: not installed (/tmp/pi)\n", readable: true, version: ShippedIntegrationVersion, fromHerdr: false},
		{name: "herdr output is garbage", text: "not herdr output\n", readable: true, version: ShippedIntegrationVersion, fromHerdr: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			version, fromHerdr := IntegrationVersion(test.text, test.readable)
			if version != test.version || fromHerdr != test.fromHerdr {
				t.Fatalf("IntegrationVersion(%q, %v) = %d, %v, want %d, %v",
					test.text, test.readable, version, fromHerdr, test.version, test.fromHerdr)
			}
		})
	}
}

func TestWithIntegrationStampsTheMarker(t *testing.T) {
	source := strings.Join([]string{
		IntegrationIDLine,
		IntegrationVersionLine(ShippedIntegrationVersion),
		ReporterVersionLine,
		"// shipped reporter body",
		"",
	}, "\n")

	stamped := string(WithIntegration([]byte(source), 10))

	want := strings.Join([]string{
		IntegrationIDLine,
		IntegrationVersionLine(10),
		ReporterVersionLine,
		"// shipped reporter body",
		"",
	}, "\n")
	if stamped != want {
		t.Fatalf("WithIntegration(10) =\n%s\nwant:\n%s", stamped, want)
	}
}

func TestWithIntegrationKeepsTheShippedDefaultForAnUnusableVersion(t *testing.T) {
	source := []byte(IntegrationIDLine + "\n" + IntegrationVersionLine(ShippedIntegrationVersion) + "\n" + ReporterVersionLine + "\n")

	for _, version := range []int{0, -3} {
		stamped := string(WithIntegration(source, version))
		if !strings.Contains(stamped, IntegrationVersionLine(ShippedIntegrationVersion)) {
			t.Fatalf("WithIntegration(%d) =\n%s\nwant the shipped default %d", version, stamped, ShippedIntegrationVersion)
		}
	}
}

func TestWithIntegrationAddsTheMarkersToASourceWithoutThem(t *testing.T) {
	source := ReporterVersionLine + "\n// reporter body\n"

	stamped := string(WithIntegration([]byte(source), 10))

	if !strings.Contains(stamped, IntegrationIDLine) {
		t.Fatalf("WithIntegration =\n%s\nwant the %q line", stamped, IntegrationIDLine)
	}
	if !strings.Contains(stamped, IntegrationVersionLine(10)) {
		t.Fatalf("WithIntegration =\n%s\nwant the %q line", stamped, IntegrationVersionLine(10))
	}
	if !strings.HasSuffix(stamped, source) {
		t.Fatalf("WithIntegration =\n%s\nwant the source bytes preserved", stamped)
	}
}
