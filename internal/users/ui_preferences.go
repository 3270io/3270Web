// SPDX-License-Identifier: AGPL-3.0-or-later
package users

import "fmt"

// UIPreferences contains only presentation choices; no role or host permissions.
type UIPreferences struct {
	WorkspaceMode       string `json:"workspaceMode,omitempty"`
	QuickStartDismissed bool   `json:"quickStartDismissed,omitempty"`
	TerminalView        string `json:"terminalView,omitempty"`
}

// UIPreferencesPatch updates only supplied fields while holding the store lock.
// Concurrent tabs changing different choices cannot overwrite each other.
type UIPreferencesPatch struct {
	WorkspaceMode       *string `json:"workspaceMode"`
	QuickStartDismissed *bool   `json:"quickStartDismissed"`
	TerminalView        *string `json:"terminalView"`
}

func (s *Store) PatchUIPreferences(id string, patch UIPreferencesPatch) (UIPreferences, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return UIPreferences{}, err
	}
	for i := range f.Users {
		if f.Users[i].ID != id {
			continue
		}
		prefs := f.Users[i].UIPreferences
		if patch.WorkspaceMode != nil {
			prefs.WorkspaceMode = *patch.WorkspaceMode
		}
		if patch.TerminalView != nil {
			prefs.TerminalView = *patch.TerminalView
		}
		if patch.QuickStartDismissed != nil {
			prefs.QuickStartDismissed = *patch.QuickStartDismissed
		}
		if prefs.WorkspaceMode != "" && prefs.WorkspaceMode != "business" && prefs.WorkspaceMode != "engineering" {
			return UIPreferences{}, fmt.Errorf("invalid workspace mode")
		}
		if prefs.TerminalView != "" && prefs.TerminalView != "readable" && prefs.TerminalView != "overview" {
			return UIPreferences{}, fmt.Errorf("invalid terminal view")
		}
		f.Users[i].UIPreferences = prefs
		return prefs, s.save(f)
	}
	return UIPreferences{}, ErrUserNotFound
}
