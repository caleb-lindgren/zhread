//go:build unix

package zhdict

import (
	"os"
	"syscall"
)

// mapFile maps path read-only. The mapping is never unmapped: a Dict lives
// as long as the process, and entries hold slices of it.
func mapFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if fi.Size() == 0 {
		return nil, nil
	}
	b, err := syscall.Mmap(int(f.Fd()), 0, int(fi.Size()),
		syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return readFile(path)
	}
	return b, nil
}
