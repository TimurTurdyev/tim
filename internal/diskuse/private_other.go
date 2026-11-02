//go:build !darwin

package diskuse

// PrivateSize: клоны файлов есть только у APFS, здесь посчитать нечего.
func PrivateSize(string) (int64, bool) { return 0, false }
