// Package gsrestore owns the GS session-restore plus: the reporter file this
// plugin ships, its installation into a gentle-shell agent directory, the
// config toggles that gate startup behaviour, and the status report.
//
// # No user knowledge
//
// Nothing here hardcodes a home directory or a user name. The agent directory
// comes from PI_CODING_AGENT_DIR of the invoking environment, and the
// gentle-shell launcher is detected by looking it up on PATH. A vanilla pi
// installation therefore simply has nothing to install into, and the reporter
// itself degrades to no resume_argv when the launcher is absent.
//
// # Default off
//
// The feature is opt-in. The startup hook reads
// <plugin-config>/config.json and does nothing unless
// gs_restore.install is true; gs_restore.install is the master gate and
// gs_restore.auto_install only chooses between installing and hinting.
package gsrestore
