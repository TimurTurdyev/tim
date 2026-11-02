package diskuse

import (
	"io/fs"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/unix"
)

// attrPrivateSize: ATTR_CMNEXT_PRIVATESIZE - блоки файла, которые не делит с ним ни один клон APFS.
// В x/sys нет обёртки getattrlist, поэтому вызов напрямую.
const attrPrivateSize = 0x8

// filePrivate: сколько места освободит удаление файла; ok=false - система не сказала (не APFS).
func filePrivate(path string) (int64, bool) {
	p, err := unix.BytePtrFromString(path)
	if err != nil {
		return 0, false
	}
	al := unix.Attrlist{Bitmapcount: unix.ATTR_BIT_MAP_COUNT, Forkattr: attrPrivateSize}
	var buf [16]byte
	_, _, e := unix.Syscall6(unix.SYS_GETATTRLIST, uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&al)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unix.FSOPT_NOFOLLOW|unix.FSOPT_ATTR_CMN_EXTENDED), 0)
	if e != 0 || *(*uint32)(unsafe.Pointer(&buf[0])) < 12 {
		return 0, false
	}
	return *(*int64)(unsafe.Pointer(&buf[4])), true
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
