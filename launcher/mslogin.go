package launcher

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"multilauncherwails/launcher/plugin"
)

const MicrosoftClientID = "ad455ae0-e6f2-40c0-b315-28e7f4f3b6b4"

const (
	msDeviceCodeURL = "https://login.microsoftonline.com/consumers/oauth2/v2.0/devicecode"
	msTokenURL      = "https://login.microsoftonline.com/consumers/oauth2/v2.0/token"
	msScope         = "XboxLive.signin offline_access"
)

type DeviceCode struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
	Message         string `json:"message"`
}

type msTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

func StartMSLogin() (*DeviceCode, error) {
	form := url.Values{
		"client_id": {MicrosoftClientID},
		"scope":     {msScope},
	}
	resp, err := http.PostForm(msDeviceCodeURL, form)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("devicecode HTTP %d: %s", resp.StatusCode, body)
	}
	var dc DeviceCode
	if err := json.Unmarshal(body, &dc); err != nil {
		return nil, err
	}
	return &dc, nil
}

type msAuthResult struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    int64
}

func PollMSLogin(ctx context.Context, deviceCode string, interval int, onProgress func(step string)) (Account, error) {
	if interval < 1 {
		interval = 5
	}
	deadline := time.Now().Add(15 * time.Minute)
	for {
		if err := ctx.Err(); err != nil {
			return Account{}, err
		}
		form := url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"client_id":   {MicrosoftClientID},
			"device_code": {deviceCode},
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, msTokenURL, strings.NewReader(form.Encode()))
		if err != nil {
			return Account{}, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return Account{}, err
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return Account{}, err
		}
		var tok msTokenResponse
		if err := json.Unmarshal(body, &tok); err != nil {
			return Account{}, err
		}
		switch {
		case tok.AccessToken != "":
			return finishMSLogin(tok.AccessToken, tok.RefreshToken, tok.ExpiresIn)
		case tok.Error == "authorization_pending":
			if time.Now().After(deadline) {
				return Account{}, fmt.Errorf("login timed out")
			}
			if err := sleepCtx(ctx, time.Duration(interval)*time.Second); err != nil {
				return Account{}, err
			}
		case tok.Error == "slow_down":
			interval++
			if err := sleepCtx(ctx, time.Duration(interval)*time.Second); err != nil {
				return Account{}, err
			}
		case tok.Error == "expired_token" || tok.Error == "authorization_declined":
			return Account{}, fmt.Errorf("login %s", tok.Error)
		default:
			return Account{}, fmt.Errorf("token error %q: %s", tok.Error, tok.ErrorDesc)
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func finishMSLogin(accessToken, refreshToken string, expiresIn int) (Account, error) {
	xbl, err := msXBL(accessToken)
	if err != nil {
		return Account{}, err
	}
	xsts, uhs, err := msXSTS(xbl)
	if err != nil {
		return Account{}, err
	}
	mcToken, err := msMinecraft(xsts, uhs)
	if err != nil {
		return Account{}, err
	}
	profile, err := msProfile(mcToken)
	if err != nil {
		return Account{}, err
	}
	acc := Account{
		ID:           profile.ID,
		Name:         profile.Name,
		Type:         "microsoft",
		AccessToken:  mcToken,
		RefreshToken: refreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(expiresIn) * time.Second).UnixMilli(),
		UserType:     "msa",
	}
	accountsMu.Lock()
	replaced := false
	for i, a := range accountsList {
		if a.ID == acc.ID {
			accountsList[i] = acc
			replaced = true
			break
		}
	}
	if !replaced {
		accountsList = append(accountsList, acc)
	}
	if activeAccID == "" {
		activeAccID = acc.ID
		os.WriteFile(activeAccountPath(), []byte(acc.ID), 0o644)
	}
	saveErr := saveAccounts()
	accountsMu.Unlock()
	if saveErr != nil {
		return acc, fmt.Errorf("save accounts: %w", saveErr)
	}
	plugin.Bus.Emit("account.added", plugin.AccountEvent{ID: acc.ID, Name: acc.Name, Type: acc.Type})
	return acc, nil
}

func msXBL(accessToken string) (string, error) {
	body := `{"Properties":{"AuthMethod":"RPS","SiteName":"user.auth.xboxlive.com","RpsTicket":"d=` + accessToken + `"},"RelyingParty":"http://auth.xboxlive.com","TokenType":"JWT"}`
	var out struct {
		Token string `json:"Token"`
	}
	if err := msPostJSON("https://user.auth.xboxlive.com/user/authenticate", body, &out); err != nil {
		return "", err
	}
	return out.Token, nil
}

func msXSTS(xblToken string) (token, uhs string, err error) {
	body := `{"Properties":{"SandboxId":"RETAIL","UserTokens":["` + xblToken + `"]},"RelyingParty":"rp://api.minecraftservices.com/","TokenType":"JWT"}`
	var out struct {
		Token         string `json:"Token"`
		DisplayClaims struct {
			XUI []struct {
				UHS string `json:"uhs"`
			} `json:"xui"`
		} `json:"DisplayClaims"`
	}
	if err := msPostJSON("https://xsts.auth.xboxlive.com/xsts/authorize", body, &out); err != nil {
		return "", "", err
	}
	if len(out.DisplayClaims.XUI) == 0 {
		return "", "", fmt.Errorf("xsts: no userhash in response")
	}
	return out.Token, out.DisplayClaims.XUI[0].UHS, nil
}

func msMinecraft(xstsToken, uhs string) (string, error) {
	body := `{"identityToken":"XBL3.0 x=` + uhs + `;` + xstsToken + `"}`
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := msPostJSON("https://api.minecraftservices.com/authentication/login_with_xbox", body, &out); err != nil {
		return "", err
	}
	return out.AccessToken, nil
}

type mcProfile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func msProfile(mcToken string) (mcProfile, error) {
	req, err := http.NewRequest("GET", "https://api.minecraftservices.com/minecraft/profile", nil)
	if err != nil {
		return mcProfile{}, err
	}
	req.Header.Set("Authorization", "Bearer "+mcToken)
	resp, err := httpClient.Do(req)
	if err != nil {
		return mcProfile{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return mcProfile{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return mcProfile{}, fmt.Errorf("profile HTTP %d: %s", resp.StatusCode, body)
	}
	var p mcProfile
	if err := json.Unmarshal(body, &p); err != nil {
		return mcProfile{}, err
	}
	return p, nil
}

func refreshMSAccount(acc Account) (Account, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {MicrosoftClientID},
		"refresh_token": {acc.RefreshToken},
		"scope":         {msScope},
	}
	resp, err := http.PostForm(msTokenURL, form)
	if err != nil {
		return acc, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return acc, err
	}
	if resp.StatusCode != http.StatusOK {
		return acc, fmt.Errorf("token refresh HTTP %d: %s", resp.StatusCode, body)
	}
	var tok msTokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return acc, err
	}
	if tok.AccessToken == "" {
		return acc, fmt.Errorf("token refresh returned no access token")
	}
	xbl, err := msXBL(tok.AccessToken)
	if err != nil {
		return acc, err
	}
	xsts, uhs, err := msXSTS(xbl)
	if err != nil {
		return acc, err
	}
	mcToken, err := msMinecraft(xsts, uhs)
	if err != nil {
		return acc, err
	}
	fresh := Account{
		ID:           acc.ID,
		Name:         acc.Name,
		Type:         acc.Type,
		AccessToken:  mcToken,
		RefreshToken: tok.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second).UnixMilli(),
		UserType:     acc.UserType,
	}
	accountsMu.Lock()
	for i := range accountsList {
		if accountsList[i].ID == acc.ID {
			accountsList[i] = fresh
			break
		}
	}
	saveErr := saveAccounts()
	accountsMu.Unlock()
	if saveErr != nil {
		return fresh, fmt.Errorf("save accounts: %w", saveErr)
	}
	return fresh, nil
}

func freshAccount(acc Account) Account {
	if acc.Type != "microsoft" || acc.RefreshToken == "" {
		return acc
	}
	if acc.AccessToken != "" && (acc.ExpiresAt == 0 || acc.ExpiresAt > time.Now().UnixMilli()) {
		return acc
	}
	if fresh, err := refreshMSAccount(acc); err == nil || fresh.AccessToken != "" {
		return fresh
	}
	return acc
}

func UploadSkin(accountID, pngBase64, variant string) error {
	accountsMu.Lock()
	var acc Account
	for i := range accountsList {
		if accountsList[i].ID == accountID {
			acc = accountsList[i]
			break
		}
	}
	accountsMu.Unlock()
	if acc.ID == "" {
		return fmt.Errorf("account not found")
	}
	if acc.Type != "microsoft" || acc.AccessToken == "" {
		return fmt.Errorf("only Microsoft accounts can upload skins")
	}
	acc = freshAccount(acc)
	if acc.AccessToken == "" {
		return fmt.Errorf("cannot refresh Microsoft token, please sign in again")
	}
	raw, err := base64.StdEncoding.DecodeString(pngBase64)
	if err != nil {
		return fmt.Errorf("bad base64 skin data: %w", err)
	}

	if variant != "SLIM" {
		variant = "CLASSIC"
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("variant", variant)
	fw, err := mw.CreateFormFile("file", "skin.png")
	if err != nil {
		return err
	}
	if _, err := fw.Write(raw); err != nil {
		return err
	}
	if err := mw.Close(); err != nil {
		return err
	}

	req, err := http.NewRequest("POST", "https://api.minecraftservices.com/minecraft/profile/skins", &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+acc.AccessToken)
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("skin upload HTTP %d: %s", resp.StatusCode, body)
	}
	if err := saveLocalSkin(acc.Name, raw); err != nil {
		return fmt.Errorf("upload ok but saving local copy failed: %w", err)
	}
	return nil
}

func saveLocalSkin(accName string, raw []byte) error {
	sum := sha256.Sum256(raw)
	name := sanitize(accName) + "-" + hex.EncodeToString(sum[:8]) + ".png"
	return os.WriteFile(filepath.Join(SkinsDir(), name), raw, 0o644)
}

func DeleteLocalSkin(name string) error {
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("invalid skin file name")
	}
	path := filepath.Join(SkinsDir(), name)
	if err := os.Remove(path); err != nil {
		return err
	}
	return nil
}

type LocalSkin struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	URL  string `json:"url"`
}

func ListLocalSkins() []LocalSkin {
	entries, err := os.ReadDir(SkinsDir())
	if err != nil {
		return []LocalSkin{}
	}
	out := []LocalSkin{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".png") {
			continue
		}
		s := LocalSkin{Name: e.Name(), URL: "/skins/" + e.Name()}
		if info, err := e.Info(); err == nil {
			s.Size = info.Size()
		}
		out = append(out, s)
	}
	return out
}

func msPostJSON(u, body string, out any) error {
	req, err := http.NewRequest("POST", u, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s HTTP %d: %s", u, resp.StatusCode, raw)
	}
	return json.Unmarshal(raw, out)
}
