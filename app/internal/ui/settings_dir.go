package ui

import "path/filepath"

// betaDir is the folder holding the settings file, where the updater keeps
// its state (the name is historical).
func (a *App) betaDir() string {
	if a.Cfg == nil || a.Cfg.path == "" {
		return ""
	}
	return filepath.Dir(a.Cfg.path)
}
