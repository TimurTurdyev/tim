package diskuse

import (
	"os"
	"syscall"
	"time"
)

func accessTime(st os.FileInfo) time.Time {
	if s, ok := st.Sys().(*syscall.Stat_t); ok {
		return time.Unix(s.Atimespec.Sec, s.Atimespec.Nsec)
	}
	return st.ModTime()
}
