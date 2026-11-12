// Package session owns herdr session management: listing named sessions,
// attaching, creating, stopping, restarting and deleting them.
//
// The manager holds the lifecycle rules: names are sanitized before they reach
// herdr, destructive actions are confirmation-gated, deleting stops first, and
// the default session is protected. The switcher is the interactive surface
// over the same manager, and the table and JSON writers are its scriptable
// surface.
package session
