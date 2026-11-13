package last

import (
	"context"

	"github.com/WilberC/herdr-suite/internal/herdrmock"
)

// The refusals a toggle reports when a session has no history to go back to.
const (
	NoPreviousWorkspace = "no previous workspace recorded yet; focus another workspace first"
	NoPreviousTab       = "no previous tab recorded in this workspace yet"
)

// Refusal is a toggle that has nowhere to go. It is an operation the caller can
// fix by focusing something else first, so it maps to the refusal exit status.
type Refusal struct {
	Reason string
}

func (r Refusal) Error() string { return r.Reason }

// ToggleWorkspace focuses the previous workspace and swaps the pair, so the same
// call leads back. A failed focus leaves the state untouched.
func ToggleWorkspace(ctx context.Context, mutator herdrmock.Mutator, getenv func(string) string) error {
	path, err := Path(getenv)
	if err != nil {
		return err
	}
	state, err := Load(path)
	if err != nil {
		return err
	}
	if state.PrevWorkspace == nil || *state.PrevWorkspace == "" {
		return Refusal{Reason: NoPreviousWorkspace}
	}
	if err := mutator.FocusWorkspace(ctx, *state.PrevWorkspace); err != nil {
		return err
	}
	state.PrevWorkspace, state.CurWorkspace = state.CurWorkspace, state.PrevWorkspace
	return Save(path, state)
}

// ToggleTab focuses the previous tab and swaps the pair. A failed focus leaves
// the state untouched.
func ToggleTab(ctx context.Context, mutator herdrmock.Mutator, getenv func(string) string) error {
	path, err := Path(getenv)
	if err != nil {
		return err
	}
	state, err := Load(path)
	if err != nil {
		return err
	}
	if state.PrevTab == nil || *state.PrevTab == "" {
		return Refusal{Reason: NoPreviousTab}
	}
	if err := mutator.FocusTab(ctx, *state.PrevTab); err != nil {
		return err
	}
	state.PrevTab, state.CurTab = state.CurTab, state.PrevTab
	return Save(path, state)
}
