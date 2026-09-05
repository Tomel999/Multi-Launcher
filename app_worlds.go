package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"multilauncherwails/launcher"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) ListWorlds(instName string) []launcher.World {
	return launcher.ListWorlds(instName)
}

func (a *App) GetWorldInfo(instName, worldName string) launcher.WorldInfo {
	return launcher.GetWorldInfo(instName, worldName)
}

func (a *App) GetItemIcon(instName, mcVersion, itemID string) string {
	return launcher.GetItemIcon(instName, mcVersion, itemID)
}

func (a *App) OpenWorldFolder(instName, worldName string) error {
	return openFolder(filepath.Join(launcher.InstanceDir(instName), "saves", worldName))
}

func (a *App) DuplicateWorld(instName, worldName string) (string, error) {
	return launcher.DuplicateWorld(instName, worldName)
}

func (a *App) RenameWorld(instName, oldName, newName string) error {
	return launcher.RenameWorld(instName, oldName, newName)
}

func (a *App) DeleteWorld(instName, worldName string) error {
	return launcher.DeleteWorld(instName, worldName)
}

func (a *App) ExportWorld(instName, worldName string) (string, error) {
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		DefaultDirectory: launcher.Root(),
		DefaultFilename:  worldName + ".zip",
		Title:            "Export world",
		Filters:          []runtime.FileFilter{{DisplayName: "ZIP archive", Pattern: "*.zip"}},
	})
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", fmt.Errorf("export cancelled")
	}
	if !strings.HasSuffix(strings.ToLower(path), ".zip") {
		path += ".zip"
	}
	return path, launcher.ExportWorld(instName, worldName, path)
}

func (a *App) ImportWorld(instName string) (string, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		DefaultDirectory: launcher.Root(),
		Title:            "Import world",
		Filters:          []runtime.FileFilter{{DisplayName: "ZIP archive", Pattern: "*.zip"}},
	})
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", fmt.Errorf("import cancelled")
	}
	return launcher.ImportWorld(instName, path)
}

func (a *App) ListScreenshots(instName string) []launcher.Screenshot {
	return launcher.ListScreenshots(instName)
}

func (a *App) GetScreenshot(instName, name string) (string, error) {
	return launcher.GetScreenshot(instName, name)
}

func (a *App) DeleteScreenshot(instName, name string) error {
	return launcher.DeleteScreenshot(instName, name)
}

func (a *App) CopyScreenshotToClipboard(instName, name string) error {
	path := filepath.Join(launcher.InstanceDir(instName), "screenshots", name)
	return launcher.CopyPNGToClipboard(path)
}

func (a *App) OpenScreenshotsFolder(instName string) error {
	return openFolder(filepath.Join(launcher.InstanceDir(instName), "screenshots"))
}
