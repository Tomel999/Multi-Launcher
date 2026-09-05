package launcher

import "github.com/zalando/go-keyring"

const keyringService = "multilauncher"

func keyringSave(id, field, secret string) error {
	if secret == "" {
		return nil
	}
	return keyring.Set(keyringService, id+":"+field, secret)
}

func keyringLoad(id, field string) string {
	v, err := keyring.Get(keyringService, id+":"+field)
	if err != nil {
		return ""
	}
	return v
}

func keyringWipe(id string) {
	_ = keyring.Delete(keyringService, id+":access")
	_ = keyring.Delete(keyringService, id+":refresh")
}

func hydrateTokens(a *Account) {
	a.AccessToken = keyringLoad(a.ID, "access")
	a.RefreshToken = keyringLoad(a.ID, "refresh")
}
