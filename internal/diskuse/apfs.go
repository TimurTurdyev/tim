package diskuse

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	apfsRole = regexp.MustCompile(`APFS Volume Disk \(Role\):\s+\S+ \(([^)]*)\)`)
	apfsName = regexp.MustCompile(`Name:\s+(.+?)(?: \(Case-(?:in)?sensitive\))?$`)
	apfsSize = regexp.MustCompile(`Capacity Consumed:\s+(\d+) B`)
	apfsMnt  = regexp.MustCompile(`Mount Point:\s+(.+)$`)
)

// Volume: том APFS. Тома одного контейнера делят одно место, поэтому занятое другими томами
// уменьшает свободное на основном, хотя в его папках этого не видно.
type Volume struct {
	Name, Role, Mount string
	Size              int64
}

// ParseAPFS: diskutil apfs list -> тома контейнера, где лежит mount (обычно /System/Volumes/Data).
func ParseAPFS(out, mount string) []Volume {
	for _, c := range strings.Split(out, "+-- Container ")[1:] {
		var list []Volume
		found := false
		for _, v := range strings.Split(c, "+-> Volume ")[1:] {
			var vol Volume
			for _, line := range strings.Split(v, "\n") {
				line = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "|"))
				if m := apfsRole.FindStringSubmatch(line); m != nil {
					vol.Role = m[1]
				} else if m := apfsSize.FindStringSubmatch(line); m != nil {
					vol.Size, _ = strconv.ParseInt(m[1], 10, 64)
				} else if m := apfsMnt.FindStringSubmatch(line); m != nil && vol.Mount == "" {
					vol.Mount = m[1]
				} else if m := apfsName.FindStringSubmatch(line); m != nil && vol.Name == "" {
					vol.Name = m[1]
				}
			}
			found = found || vol.Mount == mount
			list = append(list, vol)
		}
		if found {
			return list
		}
	}
	return nil
}
