package main

import (
	"multilauncherwails/launcher"
)

func (a *App) CurseForgeSearch(query, gameVersion, sortField string, index, classID, categoryID, modLoaderType int) (string, error) {
	return launcher.CurseForgeSearch(a.curseforgeKey, query, gameVersion, sortField, index, classID, categoryID, modLoaderType)
}

func (a *App) CurseForgeCategories(classID int) (string, error) {
	return launcher.CurseForgeCategories(a.curseforgeKey, classID)
}

func (a *App) CurseForgeFiles(modID string) (string, error) {
	return launcher.CurseForgeFiles(a.curseforgeKey, modID)
}
