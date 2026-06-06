package diskuse

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
)

var acfField = regexp.MustCompile(`"(name|installdir|LastPlayed)"\s+"([^"]*)"`)

// Game: установленная игра Steam.
type Game struct {
	Name, Dir  string // Dir - папка в steamapps/common
	LastPlayed time.Time
}

// ParseACF: appmanifest_*.acf - имя игры, её папка и когда в неё играли.
func ParseACF(data string) Game {
	var g Game
	for _, m := range acfField.FindAllStringSubmatch(data, -1) {
		switch m[1] {
		case "name":
			if g.Name == "" {
				g.Name = m[2]
			}
		case "installdir":
			g.Dir = m[2]
		case "LastPlayed":
			if n, err := strconv.ParseInt(m[2], 10, 64); err == nil && n > 0 {
				g.LastPlayed = time.Unix(n, 0)
			}
		}
	}
	return g
}

// SteamGames: игры библиотеки по папке игры; common - путь к steamapps/common.
func SteamGames(common string) map[string]Game {
	files, _ := filepath.Glob(filepath.Join(filepath.Dir(common), "appmanifest_*.acf"))
	out := map[string]Game{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if g := ParseACF(string(data)); g.Dir != "" {
			out[g.Dir] = g
		}
	}
	return out
}
