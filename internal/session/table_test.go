package session

import (
	"bytes"
	"strings"
	"testing"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
)

func tableFixture() []herdrmock.Session {
	return []herdrmock.Session{
		{Name: "default", Running: true, Default: true},
		{Name: "work", Running: true},
		{Name: "scratch"},
	}
}

func TestWriteTablePrintsStableAlignedRows(t *testing.T) {
	var out bytes.Buffer

	if err := WriteTable(&out, tableFixture()); err != nil {
		t.Fatalf("WriteTable error = %v, want nil", err)
	}

	want := strings.Join([]string{
		"NAME     STATE",
		"default  running (default)",
		"work     running",
		"scratch  stopped",
		"",
	}, "\n")
	if got := out.String(); got != want {
		t.Fatalf("WriteTable =\n%s\nwant:\n%s", got, want)
	}
}

func TestWriteTablePadsToTheWidestName(t *testing.T) {
	var out bytes.Buffer
	sessions := []herdrmock.Session{{Name: "a"}, {Name: "a-long-session-name", Running: true}}

	if err := WriteTable(&out, sessions); err != nil {
		t.Fatalf("WriteTable error = %v, want nil", err)
	}

	want := strings.Join([]string{
		"NAME                 STATE",
		"a                    stopped",
		"a-long-session-name  running",
		"",
	}, "\n")
	if got := out.String(); got != want {
		t.Fatalf("WriteTable =\n%s\nwant:\n%s", got, want)
	}
}

func TestWriteTableWithNoSessionsPrintsTheHeaderOnly(t *testing.T) {
	var out bytes.Buffer

	if err := WriteTable(&out, nil); err != nil {
		t.Fatalf("WriteTable error = %v, want nil", err)
	}

	if got, want := out.String(), "NAME  STATE\n"; got != want {
		t.Fatalf("WriteTable = %q, want %q", got, want)
	}
}

func TestWriteJSONMirrorsHerdrSessionFields(t *testing.T) {
	var out bytes.Buffer

	if err := WriteJSON(&out, []herdrmock.Session{{Name: "default", Running: true, Default: true}}); err != nil {
		t.Fatalf("WriteJSON error = %v, want nil", err)
	}

	want := strings.Join([]string{
		"{",
		`  "sessions": [`,
		"    {",
		`      "name": "default",`,
		`      "running": true,`,
		`      "default": true`,
		"    }",
		"  ]",
		"}",
		"",
	}, "\n")
	if got := out.String(); got != want {
		t.Fatalf("WriteJSON =\n%s\nwant:\n%s", got, want)
	}
}

func TestWriteJSONWithNoSessionsPrintsAnEmptyList(t *testing.T) {
	var out bytes.Buffer

	if err := WriteJSON(&out, nil); err != nil {
		t.Fatalf("WriteJSON error = %v, want nil", err)
	}

	if got, want := out.String(), "{\n  \"sessions\": []\n}\n"; got != want {
		t.Fatalf("WriteJSON = %q, want %q", got, want)
	}
}
