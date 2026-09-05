package main

import (
	"multilauncherwails/launcher"
)

func (a *App) InstallModpack(instName, source, packURL string) (launcher.ModpackInfo, error) {
	a.installMu.Lock()
	defer a.installMu.Unlock()
	return launcher.FetchAndInstallModpack(instName, source, packURL, a.curseforgeKey, a.emitProgress)
}
