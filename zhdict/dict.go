package zhdict

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// Dict is the dictionary directory's files, read once and shared by every
// probe made against it. A one-off lookup makes one probe; the reader makes
// one per batch of paragraphs, and re-reading 49 MB for each would dominate.
//
// On Unix the files are memory-mapped rather than read, so their pages
// belong to the page cache and are shared with every other process reading
// them, instead of counting 49 MB against this one.
type Dict struct {
	dir  string
	bufs [][]byte // one per entry of sources

	freqOnce, pronOnce sync.Once
	freq               []byte
	pron               [][]byte // one per entry of pronPaths
	exclude            []byte   // excludePath
	freqErr, pronErr   error

	extraOnce sync.Once
	extra     [][]byte // the extra sources, at their indices in sources
	extraErr  error
}

// Open maps the definition sources. The frequency and reading tables are
// mapped on first use, since a lookup that needs neither should not fail
// for want of them.
func Open(dir string) (*Dict, error) {
	d := &Dict{dir: dir}
	d.bufs = make([][]byte, len(sources))
	for i, s := range sources {
		if s.extra {
			continue
		}
		buf, err := mapFile(filepath.Join(dir, s.path))
		if err != nil {
			return nil, err
		}
		d.bufs[i] = buf
	}
	return d, nil
}

// CheckExtra maps the extra sources, reporting one that cannot be. A
// missing one is not an error: the extras are built separately (see
// dict/README), and a lookup without one has its other sources' entries.
func (d *Dict) CheckExtra() error {
	_, err := d.extraBufs()
	return err
}

// MissingOptional lists the optional files absent from the directory: the
// extra sources, the reading tables and the fallback exclusions. A lookup
// works without them, with fewer definitions and readings.
func (d *Dict) MissingOptional() []string {
	var paths []string
	for _, s := range sources {
		if s.extra {
			paths = append(paths, s.path)
		}
	}
	paths = append(append(paths, pronPaths...), excludePath)
	var missing []string
	for _, p := range paths {
		_, err := os.Stat(filepath.Join(d.dir, p))
		if errors.Is(err, fs.ErrNotExist) {
			missing = append(missing, p)
		}
	}
	return missing
}

// mapOptional is mapFile for a file that may be absent: a missing file
// maps to nil, which every reader treats as empty.
func mapOptional(path string) ([]byte, error) {
	buf, err := mapFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return buf, err
}

// extraBufs maps the extra sources, on first use. A missing one is nil.
func (d *Dict) extraBufs() ([][]byte, error) {
	d.extraOnce.Do(func() {
		d.extra = make([][]byte, len(sources))
		for i, s := range sources {
			if !s.extra {
				continue
			}
			buf, err := mapOptional(filepath.Join(d.dir, s.path))
			if err != nil {
				d.extraErr = err
				return
			}
			d.extra[i] = buf
		}
	})
	return d.extra, d.extraErr
}

func (d *Dict) freqBuf() ([]byte, error) {
	d.freqOnce.Do(func() {
		d.freq, d.freqErr = mapFile(filepath.Join(d.dir, freqPath))
	})
	return d.freq, d.freqErr
}

// pronBufs maps the reading tables and the fallback exclusions, on first
// use. Each is optional: a missing table is nil, and a missing
// exclusion list excludes nothing.
func (d *Dict) pronBufs() ([][]byte, error) {
	d.pronOnce.Do(func() {
		for _, path := range pronPaths {
			buf, err := mapOptional(filepath.Join(d.dir, path))
			if err != nil {
				d.pron, d.pronErr = nil, err
				return
			}
			d.pron = append(d.pron, buf)
		}
		d.exclude, d.pronErr = mapOptional(
			filepath.Join(d.dir, excludePath))
		if d.pronErr != nil {
			d.pron = nil
		}
	})
	return d.pron, d.pronErr
}

// readFile is mapFile's fallback where mapping is unavailable.
var readFile = os.ReadFile
