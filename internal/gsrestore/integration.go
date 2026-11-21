package gsrestore

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
)

// The marker lines herdr reads from an integration file. herdr trims a line,
// strips leading '/' and '#' characters and then looks for the version marker,
// so the comment prefix is part of the file's style, not of the marker.
const (
	IntegrationIDLine         = "// HERDR_INTEGRATION_ID=pi"
	IntegrationVersionMarker  = "// HERDR_INTEGRATION_VERSION="
	ShippedIntegrationVersion = 9
)

var (
	integrationVersionPattern    = regexp.MustCompile(`(?m)^// HERDR_INTEGRATION_VERSION=\d+[ \t]*$`)
	integrationStatusLinePattern = regexp.MustCompile(`(?m)^\s*pi:\s+(.+)$`)
)

// IntegrationVersionLine is the marker line carrying version, the form herdr
// parses out of an installed integration.
func IntegrationVersionLine(version int) string {
	return IntegrationVersionMarker + strconv.Itoa(version)
}

// ParseIntegrationVersion reads the pi line of "herdr integration status" and
// returns the version herdr expects: the right-hand number of "(v9 < v10)" or
// "(legacy < v10)", and the only number of "current (v9)". Every other shape,
// including "not installed" and "needs repair", names no expected version and
// reports false.
func ParseIntegrationVersion(text string) (int, bool) {
	match := integrationStatusLinePattern.FindStringSubmatch(text)
	if match == nil {
		return 0, false
	}
	line := match[1]
	if index := strings.Index(line, "< v"); index >= 0 {
		return leadingVersion(line[index+len("< v"):])
	}
	if index := strings.Index(line, "current (v"); index >= 0 {
		return leadingVersion(line[index+len("current (v"):])
	}
	return 0, false
}

func leadingVersion(text string) (int, bool) {
	end := 0
	for end < len(text) && text[end] >= '0' && text[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, false
	}
	version, err := strconv.Atoi(text[:end])
	if err != nil {
		return 0, false
	}
	return version, true
}

// IntegrationVersion picks the version to write into the reporter: the one
// herdr reports it expects, or ShippedIntegrationVersion when herdr could not
// be asked (readable false) or its text names no expected version. fromHerdr
// reports which of the two happened.
func IntegrationVersion(text string, readable bool) (version int, fromHerdr bool) {
	if readable {
		if version, ok := ParseIntegrationVersion(text); ok {
			return version, true
		}
	}
	return ShippedIntegrationVersion, false
}

// WithIntegration stamps source with the marker herdr reads: the integration
// id and the version line. A source without those lines gains them next to the
// HERDR_SUITE_GS_RESTORE_VERSION marker instead of failing the install, so a
// reporter can never be written without them.
func WithIntegration(source []byte, version int) []byte {
	if version <= 0 {
		version = ShippedIntegrationVersion
	}
	line := []byte(IntegrationVersionLine(version))
	if integrationVersionPattern.Match(source) {
		return integrationVersionPattern.ReplaceAll(source, line)
	}

	markers := []byte(IntegrationIDLine + "\n" + string(line) + "\n")
	anchor := []byte(ReporterVersionLine)
	if index := bytes.Index(source, anchor); index >= 0 {
		stamped := make([]byte, 0, len(source)+len(markers))
		stamped = append(stamped, source[:index]...)
		stamped = append(stamped, markers...)
		return append(stamped, source[index:]...)
	}
	return append(markers, source...)
}

// integrationVersionOf reads the version an installed reporter carries, and
// reports false when the file has no marker.
func integrationVersionOf(reporter []byte) (int, bool) {
	match := integrationVersionPattern.Find(reporter)
	if match == nil {
		return 0, false
	}
	version, ok := leadingVersion(strings.TrimPrefix(string(match), IntegrationVersionMarker))
	return version, ok
}
