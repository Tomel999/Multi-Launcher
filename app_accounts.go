package main

import (
	"context"

	"multilauncherwails/launcher"
)

type PubAccount struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

func toPubAccount(acc launcher.Account) PubAccount {
	return PubAccount{ID: acc.ID, Name: acc.Name, Type: acc.Type}
}

func pubAccounts() []PubAccount {
	accs := launcher.GetAccounts()
	out := make([]PubAccount, 0, len(accs))
	for _, acc := range accs {
		out = append(out, toPubAccount(acc))
	}
	return out
}

func (a *App) GetAccounts() []PubAccount {
	return pubAccounts()
}

func (a *App) GetActiveAccount() string {
	return launcher.GetActiveAccountID()
}

func (a *App) AddOfflineAccount(name string) (PubAccount, error) {
	acc, err := launcher.AddOfflineAccount(name)
	return toPubAccount(acc), err
}

func (a *App) AddMicrosoftAccount() (*launcher.DeviceCode, error) {
	return launcher.StartMSLogin()
}

func (a *App) PollMicrosoftLogin(deviceCode string, interval int) (PubAccount, error) {
	a.mu.Lock()
	ctx, cancel := context.WithCancel(a.ctx)
	a.msLoginCancel = cancel
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.msLoginCancel = nil
		a.mu.Unlock()
	}()
	acc, err := launcher.PollMSLogin(ctx, deviceCode, interval, nil)
	return toPubAccount(acc), err
}

func (a *App) CancelMicrosoftLogin() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.msLoginCancel != nil {
		a.msLoginCancel()
		a.msLoginCancel = nil
	}
}

func (a *App) SetActiveAccount(id string) error {
	return launcher.SetActiveAccount(id)
}

func (a *App) UploadSkin(accountID, pngBase64, variant string) error {
	return launcher.UploadSkin(accountID, pngBase64, variant)
}

func (a *App) ListLocalSkins() []launcher.LocalSkin {
	return launcher.ListLocalSkins()
}

func (a *App) DeleteLocalSkin(name string) error {
	return launcher.DeleteLocalSkin(name)
}

func (a *App) DeleteAccount(id string) error {
	return launcher.DeleteAccount(id)
}
