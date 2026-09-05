package launcher

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

func CurseForgeSearch(apiKey, query, gameVersion, sortField string, index, classID, categoryID, modLoaderType int) (string, error) {
	if apiKey == "" {
		return "", fmt.Errorf("CurseForge API key not set")
	}
	if classID == 0 {
		classID = 4471
	}
	u := "https://api.curseforge.com/v1/mods/search?gameId=432&classId=" + fmt.Sprint(classID) + "&pageSize=50"
	if index > 0 {
		u += "&index=" + fmt.Sprint(index)
	}
	if query != "" {
		u += "&searchFilter=" + url.QueryEscape(query)
	}
	if gameVersion != "" {
		u += "&gameVersion=" + url.QueryEscape(gameVersion)
	}
	if sortField != "" {
		u += "&sortField=" + url.QueryEscape(sortField) + "&sortOrder=Desc"
	}
	if categoryID > 0 {
		u += "&categoryId=" + fmt.Sprint(categoryID)
	}
	if modLoaderType > 0 {
		u += "&modLoaderType=" + fmt.Sprint(modLoaderType)
	}
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-API-Key", apiKey)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("CurseForge HTTP %d: %s", resp.StatusCode, string(body))
	}
	return string(body), nil
}

func CurseForgeCategories(apiKey string, classID int) (string, error) {
	if apiKey == "" {
		return "", fmt.Errorf("CurseForge API key not set")
	}
	u := "https://api.curseforge.com/v1/categories?gameId=432"
	if classID > 0 {
		u += "&classId=" + fmt.Sprint(classID)
	}
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-API-Key", apiKey)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("CurseForge HTTP %d: %s", resp.StatusCode, string(body))
	}
	return string(body), nil
}

func CurseForgeFiles(apiKey string, modID string) (string, error) {
	if apiKey == "" {
		return "", fmt.Errorf("CurseForge API key not set")
	}
	u := "https://api.curseforge.com/v1/mods/" + url.PathEscape(modID) + "/files?pageSize=50"
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-API-Key", apiKey)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("CurseForge HTTP %d: %s", resp.StatusCode, string(body))
	}
	return string(body), nil
}

type curseForgeFile struct {
	ID          int    `json:"id"`
	ModID       int    `json:"modId"`
	FileName    string `json:"fileName"`
	FileLength  int64  `json:"fileLength"`
	DownloadURL string `json:"downloadUrl"`
}

func curseForgeBatch(apiKey string, ids []int) ([]ModpackFile, error) {
	body, err := json.Marshal(map[string][]int{"fileIds": ids})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", "https://api.curseforge.com/v1/mods/files", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", apiKey)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("CurseForge batch HTTP %d: %s", resp.StatusCode, string(raw))
	}
	var out struct {
		Data []curseForgeFile `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	byID := make(map[int]curseForgeFile, len(out.Data))
	for _, f := range out.Data {
		byID[f.ID] = f
	}
	files := make([]ModpackFile, 0, len(ids))
	for _, id := range ids {
		f, ok := byID[id]
		if !ok || f.DownloadURL == "" {
			continue
		}
		files = append(files, ModpackFile{
			Path: filepath.ToSlash(filepath.Join("mods", f.FileName)),
			URL:  f.DownloadURL,
			Size: f.FileLength,
		})
	}
	return files, nil
}

func resolveCurseForgeFiles(apiKey string, m *curseForgeManifest) ([]ModpackFile, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("CurseForge API key not set")
	}
	files := make([]ModpackFile, 0, len(m.Files))
	const batchSize = 50
	for i := 0; i < len(m.Files); i += batchSize {
		end := i + batchSize
		if end > len(m.Files) {
			end = len(m.Files)
		}
		ids := make([]int, 0, end-i)
		for _, f := range m.Files[i:end] {
			if f.FileID > 0 {
				ids = append(ids, f.FileID)
			}
		}
		if len(ids) == 0 {
			continue
		}
		resolved, err := curseForgeBatch(apiKey, ids)
		if err != nil {
			return nil, err
		}
		files = append(files, resolved...)
	}
	if len(m.Files) > 0 && len(files) == 0 {
		return nil, fmt.Errorf("no CurseForge files resolved (API key invalid?)")
	}
	return files, nil
}
