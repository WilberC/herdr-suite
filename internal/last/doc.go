// Package last keeps the focus history behind the "previous workspace" and
// "previous tab" toggles.
//
// The plugin declares two event hooks, workspace.focused and tab.focused, that
// run "hs last observe". Each hook appends one id to a per-session state file and
// exits; no process stays resident between events. The toggles read the same
// file, focus the recorded previous id through the herdr CLI, and swap the pair
// so the same key leads back.
//
// The state file is keyed by the herdr socket path, so two sessions never share
// one history. Every write goes through a temporary file in the same directory
// and a rename, so a reader sees the old state or the new one, never a partial
// document. A missing or corrupt file is an empty history, which is what a fresh
// session has.
package last
