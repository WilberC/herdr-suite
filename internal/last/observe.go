package last

import "encoding/json"

// The event kinds the plugin subscribes to. herdr names the kind in
// $HERDR_PLUGIN_EVENT and repeats the payload in $HERDR_PLUGIN_EVENT_JSON.
const (
	KindWorkspace = "workspace.focused"
	KindTab       = "tab.focused"
)

// Observe reads one event payload and records the id it carries. An unknown
// kind, a missing or malformed payload, or a payload without the id is a no-op:
// a focus event must never fail because of this command.
func Observe(getenv func(string) string, kind, payload string) error {
	id, ok := focusedID(kind, payload)
	if !ok {
		return nil
	}
	return Record(getenv, kind, id)
}

// Record moves one pair forward: the incoming id becomes current and the id it
// replaced becomes previous. A repeat of the current id is a no-op, so a focus
// event that herdr emits twice never collapses the pair into itself.
func Record(getenv func(string) string, kind, id string) error {
	if id == "" {
		return nil
	}
	switch kind {
	case KindWorkspace, KindTab:
	default:
		return nil
	}

	path, err := Path(getenv)
	if err != nil {
		return err
	}
	state, err := Load(path)
	if err != nil {
		return err
	}

	switch kind {
	case KindWorkspace:
		if state.CurWorkspace != nil && *state.CurWorkspace == id {
			return nil
		}
		state.PrevWorkspace = state.CurWorkspace
		state.CurWorkspace = &id
	case KindTab:
		if state.CurTab != nil && *state.CurTab == id {
			return nil
		}
		state.PrevTab = state.CurTab
		state.CurTab = &id
	}
	return Save(path, state)
}

// focusedID reads the id one event payload carries. herdr serializes the event
// envelope as {"event":...,"data":{...}}, and the focused id lives in that data
// object: workspace_id for a workspace event, tab_id for a tab event.
func focusedID(kind, payload string) (string, bool) {
	var id string
	switch kind {
	case KindWorkspace:
	case KindTab:
	default:
		return "", false
	}

	var document struct {
		Data struct {
			WorkspaceID string `json:"workspace_id"`
			TabID       string `json:"tab_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(payload), &document); err != nil {
		return "", false
	}
	if kind == KindWorkspace {
		id = document.Data.WorkspaceID
	} else {
		id = document.Data.TabID
	}
	return id, id != ""
}
