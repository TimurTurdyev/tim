package release

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/TimurTurdyev/tim/internal/ui"
)

const latestAPI = "https://api.github.com/repos/TimurTurdyev/tim/releases/latest"

var client = &http.Client{Timeout: 60 * time.Second}

// Latest: последний релиз на GitHub.
func Latest() (Release, error) {
	var rel Release
	data, err := get(latestAPI, 1<<20)
	if err != nil {
		return rel, err
	}
	return rel, json.Unmarshal(data, &rel)
}

// Checksum: сумма архива из checksums.txt релиза; пусто - суммы нет, и ставить такое нельзя.
func Checksum(rel Release, asset string) (string, error) {
	for _, a := range rel.Assets {
		if a.Name != "checksums.txt" {
			continue
		}
		body, err := get(a.URL, 1<<20)
		if err != nil {
			return "", err
		}
		return ParseChecksums(bytes.NewReader(body))[asset], nil
	}
	return "", nil
}

// Binary скачивает архив, сверяет его с суммой и достаёт из него tim.
func Binary(asset Asset, sum string) ([]byte, error) {
	archive, err := get(asset.URL, 200<<20)
	if err != nil {
		return nil, err
	}
	got := sha256.Sum256(archive)
	if hex.EncodeToString(got[:]) != sum {
		return nil, ui.Errorf("сумма %s не совпала с checksums.txt, обновление отменено", asset.Name)
	}
	return ExtractTar(bytes.NewReader(archive), "tim")
}

func get(url string, limit int64) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, ui.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}
