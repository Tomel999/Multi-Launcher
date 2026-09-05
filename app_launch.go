package main

import (
	"fmt"

	"multilauncherwails/launcher"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) LaunchInstance(instName, version, loader, loaderVersion string, opts launcher.LaunchOptions) error {
	a.launchMu.Lock()
	defer a.launchMu.Unlock()
	if launcher.IsRunning() {
		return fmt.Errorf("game already running")
	}
	a.resetLaunch()
	launcher.ResetInstallCancel()
	go launcher.LaunchVanillaFlow(instName, version, loader, loaderVersion, opts, a.emitLog, a.emitState, a.emitProgress)
	return nil
}

func (a *App) TestJavaPath(path string) (string, error) {
	return launcher.TestJava(path)
}

func (a *App) Clients() []launcher.ClientInfo {
	return launcher.Clients()
}

func (a *App) ClientVersions(clientID string) ([]launcher.ClientVersion, error) {
	return launcher.ClientVersions(clientID)
}

func (a *App) LaunchClientInstance(instName, clientID, version, module string, opts launcher.LaunchOptions) error {
	a.launchMu.Lock()
	defer a.launchMu.Unlock()
	if launcher.IsRunning() {
		return fmt.Errorf("game already running")
	}
	a.resetLaunch()
	launcher.ResetInstallCancel()
	go func() {
		acc := launcher.ResolveAccount(opts.AccountID)
		if err := launcher.LaunchClient(instName, clientID, version, module, acc, opts, a.emitLog, a.emitState, a.emitProgress); err != nil {
			a.emitLog("Client error: " + err.Error())
			a.emitState(false, 0, err.Error())
		}
	}()
	return nil
}

func (a *App) PickJavaFile() (string, error) {
	p, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Select Java executable",
		Filters: []runtime.FileFilter{{DisplayName: "Java executables", Pattern: "*.exe"}},
	})
	if err != nil {
		return "", err
	}
	return p, nil
}

func (a *App) StopInstance() error {
	launcher.Stop()
	return nil
}

// CancelInstall aborts in-progress game file downloads during launch
// preparation. The launch flow ends with an "installation canceled" error.
func (a *App) CancelInstall() error {
	launcher.CancelInstall()
	return nil
}

func (a *App) IsGameRunning() bool {
	return launcher.IsRunning()
}

func (a *App) SetDownloadConcurrency(n int) {
	launcher.SetDownloadConcurrency(n)
}

func (a *App) GetDownloadConcurrency() int {
	return launcher.GetDownloadConcurrency()
}
