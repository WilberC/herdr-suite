package session

import (
	"errors"
	"testing"
)

func TestSanitizeSessionName(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr string
	}{
		{
			name: "a plain name is unchanged",
			raw:  "work",
			want: "work",
		},
		{
			name: "a single space becomes a dash",
			raw:  "my project",
			want: "my-project",
		},
		{
			name: "whitespace runs collapse to one dash",
			raw:  "  my \t  project ",
			want: "my-project",
		},
		{
			name: "a newline collapses like any whitespace",
			raw:  "a\nb",
			want: "a-b",
		},
		{
			name: "allowed punctuation is kept",
			raw:  "v1.2_x-y",
			want: "v1.2_x-y",
		},
		{
			name:    "an empty name is refused",
			raw:     "",
			wantErr: "session name is empty",
		},
		{
			name:    "whitespace only is refused",
			raw:     "   \t ",
			wantErr: "session name is empty",
		},
		{
			name:    "dashes only are refused",
			raw:     " - - ",
			wantErr: "session name is empty",
		},
		{
			name:    "a rejected character reports the sanitized name",
			raw:     "my project!",
			wantErr: "invalid session name 'my-project!'; use letters, digits, '.', '_' and '-'",
		},
		{
			name:    "another rejected character is refused too",
			raw:     "work/1",
			wantErr: "invalid session name 'work/1'; use letters, digits, '.', '_' and '-'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Sanitize(tt.raw)

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Sanitize(%q) error = %v, want nil", tt.raw, err)
				}
				if got != tt.want {
					t.Fatalf("Sanitize(%q) = %q, want %q", tt.raw, got, tt.want)
				}
				return
			}

			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("Sanitize(%q) error = %v, want %q", tt.raw, err, tt.wantErr)
			}
			var refusal Refusal
			if !errors.As(err, &refusal) {
				t.Fatalf("Sanitize(%q) error type = %T, want Refusal", tt.raw, err)
			}
			if got != "" {
				t.Fatalf("Sanitize(%q) = %q on error, want empty", tt.raw, got)
			}
		})
	}
}

func TestSanitizeNameWordsTheRefusalForItsKind(t *testing.T) {
	got, err := SanitizeName("my layout!", "layout")

	if got != "" {
		t.Fatalf("SanitizeName = %q on error, want empty", got)
	}
	want := "invalid layout name 'my-layout!'; use letters, digits, '.', '_' and '-'"
	if err == nil || err.Error() != want {
		t.Fatalf("SanitizeName error = %v, want %q", err, want)
	}

	name, err := SanitizeName("my layout", "layout")
	if err != nil || name != "my-layout" {
		t.Fatalf("SanitizeName = %q, %v, want %q without error", name, err, "my-layout")
	}
}
