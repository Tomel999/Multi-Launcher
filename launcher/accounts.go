package launcher

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/google/uuid"

	"multilauncherwails/launcher/plugin"
)

type Account struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	AccessToken  string `json:"accessToken,omitempty"`
	RefreshToken string `json:"refreshToken,omitempty"`
	ExpiresAt    int64  `json:"expiresAt,omitempty"`
	UserType     string `json:"userType,omitempty"`
}

var (
	accountsMu   sync.Mutex
	activeAccID  string
	accountsList []Account
)

func accountsPath() string {
	return filepath.Join(filepath.Dir(root()), "config", "accounts.json")
}

func LoadAccounts() []Account {
	accountsMu.Lock()
	defer accountsMu.Unlock()
	accountsList = nil
	activeAccID = ""
	if data, err := os.ReadFile(accountsPath()); err == nil {
		_ = json.Unmarshal(data, &accountsList)
	}
	migrated := false
	saveFailed := false
	for i := range accountsList {
		a := &accountsList[i]
		if a.AccessToken != "" || a.RefreshToken != "" {
			errA := keyringSave(a.ID, "access", migrateLegacySecret(a.AccessToken))
			errR := keyringSave(a.ID, "refresh", migrateLegacySecret(a.RefreshToken))
			if errA != nil || errR != nil {
				// keep legacy values in the file, retry next start
				saveFailed = true
				continue
			}
			a.AccessToken = ""
			a.RefreshToken = ""
			migrated = true
		}
	}
	if migrated && !saveFailed {
		writeAccountsFile()
	}
	for i := range accountsList {
		hydrateTokens(&accountsList[i])
	}
	if data, err := os.ReadFile(activeAccountPath()); err == nil {
		activeAccID = strings.TrimSpace(string(data))
	}
	return append([]Account(nil), accountsList...)
}

// migrateLegacySecret moves pre-keyring secrets over: plain values pass
// through, dpapi:/aes: payloads died with the old crypto and need a re-login.
func migrateLegacySecret(s string) string {
	if strings.HasPrefix(s, "dpapi:") || strings.HasPrefix(s, "aes:v1:") {
		return ""
	}
	return s
}

func writeAccountsFile() {
	os.MkdirAll(filepath.Dir(accountsPath()), 0o755)
	enc := make([]Account, len(accountsList))
	for i, a := range accountsList {
		a.AccessToken = ""
		a.RefreshToken = ""
		enc[i] = a
	}
	if data, err := json.MarshalIndent(enc, "", "  "); err == nil {
		os.WriteFile(accountsPath(), data, 0o600)
	}
}

func activeAccountPath() string {
	return filepath.Join(filepath.Dir(root()), "config", "active_account.txt")
}

func saveAccounts() error {
	os.MkdirAll(filepath.Dir(accountsPath()), 0o755)
	enc := make([]Account, len(accountsList))
	for i, a := range accountsList {
		errA := keyringSave(a.ID, "access", a.AccessToken)
		errR := keyringSave(a.ID, "refresh", a.RefreshToken)
		if errA != nil || errR != nil {
			// keep plaintext as fallback so a broken keyring never destroys tokens; retried on next save
			enc[i] = a
			continue
		}
		a.AccessToken = ""
		a.RefreshToken = ""
		enc[i] = a
	}
	data, err := json.MarshalIndent(enc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(accountsPath(), data, 0o600)
}

func GetAccounts() []Account {
	accountsMu.Lock()
	defer accountsMu.Unlock()
	out := make([]Account, len(accountsList))
	for i, a := range accountsList {
		a.AccessToken = ""
		a.RefreshToken = ""
		out[i] = a
	}
	return out
}

func GetActiveAccountID() string {
	accountsMu.Lock()
	defer accountsMu.Unlock()
	return activeAccID
}

// AccountByID returns the account with the given ID (tokens stripped).
func AccountByID(id string) Account {
	accountsMu.Lock()
	defer accountsMu.Unlock()
	for _, a := range accountsList {
		if a.ID == id {
			a.AccessToken = ""
			a.RefreshToken = ""
			return a
		}
	}
	return Account{}
}

// ResolveAccount returns the account for a launch: the per-instance account
// when set and existing, otherwise the globally active account. Unlike the
// getters used by the UI it does NOT strip secrets — the game needs a valid
// Minecraft session token — and Microsoft tokens are refreshed when expired,
// so a game started after days of inactivity still authenticates as premium.
func ResolveAccount(id string) Account {
	target := id
	if target == "" {
		accountsMu.Lock()
		target = activeAccID
		accountsMu.Unlock()
	}
	if target != "" {
		if acc := accountForLaunch(target); acc.ID != "" {
			return acc
		}
	}
	// Fall back to the active account when the requested one is unknown.
	accountsMu.Lock()
	fallback := activeAccID
	accountsMu.Unlock()
	if fallback != "" {
		return accountForLaunch(fallback)
	}
	return Account{}
}

// accountForLaunch returns the account with the given ID, with tokens
// hydrated from the keyring, and — for Microsoft accounts — refreshed when the
// stored session token has expired. It must be called without accountsMu held:
// freshAccount -> refreshMSAccount locks it itself.
func accountForLaunch(id string) Account {
	var acc Account
	accountsMu.Lock()
	for _, a := range accountsList {
		if a.ID == id {
			acc = a
			break
		}
	}
	accountsMu.Unlock()
	if acc.ID == "" {
		return Account{}
	}
	if acc.Type != "microsoft" {
		return acc
	}
	return freshAccount(acc)
}

func ActiveAccount() Account {
	accountsMu.Lock()
	defer accountsMu.Unlock()
	for _, a := range accountsList {
		if a.ID == activeAccID {
			a.AccessToken = ""
			a.RefreshToken = ""
			return a
		}
	}
	return Account{}
}

func SetActiveAccount(id string) error {
	accountsMu.Lock()
	defer accountsMu.Unlock()
	found := false
	for _, acc := range accountsList {
		if acc.ID == id {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("account %q not found", id)
	}
	activeAccID = id
	os.MkdirAll(filepath.Dir(activeAccountPath()), 0o755)
	return os.WriteFile(activeAccountPath(), []byte(id), 0o644)
}

var validName = regexp.MustCompile(`^[A-Za-z0-9_]{3,16}$`)

func AddOfflineAccount(name string) (Account, error) {
	name = strings.TrimSpace(name)
	if !validName.MatchString(name) {
		return Account{}, fmt.Errorf("name must be 3-16 chars of letters, digits or _")
	}
	acc := Account{
		ID:       uuid.NewString(),
		Name:     name,
		Type:     "offline",
		UserType: "legacy",
	}
	accountsMu.Lock()
	accountsList = append(accountsList, acc)
	saveErr := saveAccounts()
	if activeAccID == "" {
		activeAccID = acc.ID
		os.WriteFile(activeAccountPath(), []byte(acc.ID), 0o644)
	}
	accountsMu.Unlock()
	if saveErr != nil {
		return Account{}, fmt.Errorf("save accounts: %w", saveErr)
	}
	plugin.Bus.Emit("account.added", plugin.AccountEvent{ID: acc.ID, Name: acc.Name, Type: acc.Type})
	return acc, nil
}

func DeleteAccount(id string) error {
	accountsMu.Lock()
	defer accountsMu.Unlock()
	out := accountsList[:0]
	found := false
	for _, acc := range accountsList {
		if acc.ID == id {
			found = true
			continue
		}
		out = append(out, acc)
	}
	if !found {
		return fmt.Errorf("account %q not found", id)
	}
	accountsList = out
	keyringWipe(id)
	if activeAccID == id {
		activeAccID = ""
		os.Remove(activeAccountPath())
	}
	saveErr := saveAccounts()
	plugin.Bus.Emit("account.removed", plugin.AccountEvent{ID: id})
	if saveErr != nil {
		return fmt.Errorf("save accounts: %w", saveErr)
	}
	return nil
}

func AccountByName(name string) Account {
	accountsMu.Lock()
	var acc Account
	found := false
	if activeAccID != "" {
		for _, a := range accountsList {
			if a.ID == activeAccID {
				acc = a
				found = true
				break
			}
		}
	}
	if !found {
		for _, a := range accountsList {
			if a.Name == name {
				acc = a
				found = true
				break
			}
		}
	}
	accountsMu.Unlock()
	if !found {
		return Account{Name: name, UserType: "legacy"}
	}
	return freshAccount(acc)
}
