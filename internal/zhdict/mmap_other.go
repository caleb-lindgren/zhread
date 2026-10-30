//go:build !unix

package zhdict

func mapFile(path string) ([]byte, error) { return readFile(path) }
