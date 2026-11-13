package diskuse

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"unsafe"

	"github.com/TimurTurdyev/tim/internal/par"
	"golang.org/x/sys/unix"
)

// Атрибуты getattrlist; в x/sys нет ни обёртки вызова, ни расширенных атрибутов APFS.
const (
	attrFileID      = 0x02000000 // ATTR_CMN_FILEID
	attrAllocSize   = 0x4        // ATTR_FILE_ALLOCSIZE
	attrPrivateSize = 0x8        // ATTR_CMNEXT_PRIVATESIZE - блоки, которые не делит ни один клон
	attrCloneID     = 0x100      // ATTR_CMNEXT_CLONEID - семья клонов
	attrCloneRefs   = 0x1000     // ATTR_CMNEXT_CLONE_REFCNT - сколько файлов в семье
)

// fileShare: сколько файл занимает на диске и с кем делит это место.
type fileShare struct {
	id             uint64 // номер файла: жёсткие ссылки на него - один файл
	alloc, private int64
	clone          uint64
	refs           uint32
}

func getattr(path string, al unix.Attrlist, buf []byte) bool {
	p, err := unix.BytePtrFromString(path)
	if err != nil {
		return false
	}
	_, _, e := unix.Syscall6(unix.SYS_GETATTRLIST, uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&al)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unix.FSOPT_NOFOLLOW|unix.FSOPT_ATTR_CMN_EXTENDED), 0)
	return e == 0 && int(*(*uint32)(unsafe.Pointer(&buf[0]))) >= len(buf)
}

// filePrivate: сколько места освободит удаление файла; ok=false - система не сказала (не APFS).
func filePrivate(path string) (int64, bool) {
	var buf [12]byte
	if !getattr(path, unix.Attrlist{Bitmapcount: unix.ATTR_BIT_MAP_COUNT, Forkattr: attrPrivateSize}, buf[:]) {
		return 0, false
	}
	return *(*int64)(unsafe.Pointer(&buf[4])), true
}

func share(path string) (fileShare, bool) {
	var buf [40]byte
	al := unix.Attrlist{Bitmapcount: unix.ATTR_BIT_MAP_COUNT, Commonattr: attrFileID, Fileattr: attrAllocSize,
		Forkattr: attrPrivateSize | attrCloneID | attrCloneRefs}
	if !getattr(path, al, buf[:]) {
		return fileShare{}, false
	}
	return fileShare{id: *(*uint64)(unsafe.Pointer(&buf[4])), alloc: *(*int64)(unsafe.Pointer(&buf[12])),
		private: *(*int64)(unsafe.Pointer(&buf[20])), clone: *(*uint64)(unsafe.Pointer(&buf[28])), refs: *(*uint32)(unsafe.Pointer(&buf[36]))}, true
}

// PrivateSize: сколько освободит удаление папки - без блоков, общих с клонами (cp -c, копия в Finder).
// ok=false - хоть про один файл система не сказала, считать нечего.
func PrivateSize(dir string) (int64, bool) {
	var sum int64
	ok := true
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !ok || !d.Type().IsRegular() {
			return nil
		}
		n, good := filePrivate(p)
		if !good {
			ok = false
			return filepath.SkipAll
		}
		sum += n
		return nil
	})
	return sum, ok
}

type ownFile struct {
	path, top string
	fileShare
}

// OwnTree: размеры папок под dirs, где общие блоки клонов APFS посчитаны один раз, а не у каждого клона.
// Семья, у которой есть файл снаружи dirs, не считается вовсе: тот файл уже посчитан целиком.
func OwnTree(dirs []string) (Tree, bool) {
	var units []ownFile
	for _, d := range dirs {
		entries, err := os.ReadDir(d)
		if err != nil {
			return nil, false
		}
		for _, e := range entries {
			units = append(units, ownFile{path: filepath.Join(d, e.Name()), top: d})
		}
	}
	files := slices.Concat(par.Map(units, par.Limit, func(u ownFile) []ownFile { return shares(u.path, u.top) })...)
	slices.SortFunc(files, func(a, b ownFile) int { return strings.Compare(a.path, b.path) })
	seen := map[uint64]bool{}
	members := map[uint64]int{}
	files = slices.DeleteFunc(files, func(f ownFile) bool {
		if seen[f.id] {
			return true
		}
		seen[f.id] = true
		members[f.clone]++
		return false
	})
	t := Tree{}
	for _, d := range dirs {
		t[d] = 0
	}
	counted := map[uint64]bool{}
	for _, f := range files {
		own := f.private
		if shared := f.alloc - f.private; shared > 0 && int(f.refs) <= members[f.clone] && !counted[f.clone] {
			counted[f.clone] = true
			own += shared
		}
		for a := filepath.Dir(f.path); ; a = filepath.Dir(a) {
			t[a] += own
			if a == f.top {
				break
			}
		}
	}
	return t, true
}

// shares: файлы под p; о ком система не сказала - по блокам, как у du.
func shares(p, top string) []ownFile {
	var out []ownFile
	_ = filepath.WalkDir(p, func(f string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return nil
		}
		s, ok := share(f)
		if !ok {
			info, err := d.Info()
			if err != nil {
				return nil
			}
			st, _ := info.Sys().(*syscall.Stat_t)
			if st == nil {
				return nil
			}
			s = fileShare{id: st.Ino, alloc: st.Blocks * 512, private: st.Blocks * 512}
		}
		out = append(out, ownFile{path: f, top: top, fileShare: s})
		return nil
	})
	return out
}
