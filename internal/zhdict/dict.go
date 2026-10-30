package zhdict

import (
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
	freq, pron         []byte
	freqErr, pronErr   error
}

// Open maps the definition sources. The frequency and reading tables are
// mapped on first use, since a lookup that needs neither should not fail
// for want of them.
func Open(dir string) (*Dict, error) {
	d := &Dict{dir: dir}
	for _, s := range sources {
		buf, err := mapFile(filepath.Join(dir, s.path))
		if err != nil {
			return nil, err
		}
		d.bufs = append(d.bufs, buf)
	}
	return d, nil
}

func (d *Dict) freqBuf() ([]byte, error) {
	d.freqOnce.Do(func() {
		d.freq, d.freqErr = mapFile(filepath.Join(d.dir, freqPath))
	})
	return d.freq, d.freqErr
}

func (d *Dict) pronBuf() ([]byte, error) {
	d.pronOnce.Do(func() {
		d.pron, d.pronErr = mapFile(filepath.Join(d.dir, pronPath))
	})
	return d.pron, d.pronErr
}

// readFile is mapFile's fallback where mapping is unavailable.
var readFile = os.ReadFile
