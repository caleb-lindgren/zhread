package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Bookmarks are kept one per file, in $XDG_STATE_HOME/zhread/bookmarks
// (~/.local/state by default): "path\tline\ttext", the path absolute and
// the text the start of the marked line. Setting a bookmark replaces the
// file's previous one. The text lets the mark find its line again after
// the file has been edited above it.

const markText = 30 // characters of the line kept to find it again

func bookmarkPath() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "zhread", "bookmarks")
}

func readMarks() map[string][2]string {
	m := map[string][2]string{}
	b, _ := os.ReadFile(bookmarkPath())
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.SplitN(l, "\t", 3)
		if len(f) == 3 {
			m[f[0]] = [2]string{f[1], f[2]}
		}
	}
	return m
}

// getMark returns the line the file's bookmark is on, or -1.
func (d *doc) getMark() int {
	abs, _ := filepath.Abs(d.path)
	m, ok := readMarks()[abs]
	if !ok {
		return -1
	}
	n, _ := strconv.Atoi(m[0])
	if n < len(d.lines) && strings.HasPrefix(string(d.lines[n]), m[1]) {
		return n
	}
	for i, l := range d.lines {
		if m[1] != "" && strings.HasPrefix(string(l), m[1]) {
			return i
		}
	}
	return min(n, len(d.lines)-1)
}

func (d *doc) setMark(line int) error {
	abs, _ := filepath.Abs(d.path)
	m := readMarks()
	text := d.lines[line]
	text = text[:min(len(text), markText)]
	m[abs] = [2]string{strconv.Itoa(line), string(text)}
	var sb strings.Builder
	for k, v := range m {
		sb.WriteString(k + "\t" + v[0] + "\t" + v[1] + "\n")
	}
	p := bookmarkPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(sb.String()), 0o644)
}
