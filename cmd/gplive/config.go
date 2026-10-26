package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func dokkuRoot() string {
	if v := os.Getenv("DOKKU_ROOT"); v != "" {
		return v
	}
	return "/home/dokku"
}

func cacheDir() string { return filepath.Join(dokkuRoot(), ".gitpushcache") }

// serverURLs returns the gitpushlive servers from the URLS file (space separated).
func serverURLs() []string {
	b, err := os.ReadFile(filepath.Join(cacheDir(), "URLS"))
	if err != nil {
		return nil
	}
	return strings.Fields(string(b))
}

// tryLock takes a non-blocking exclusive flock on the named file in the cache
// dir. It returns nil if another process already holds it.
func tryLock(name string) *os.File {
	f, err := os.OpenFile(filepath.Join(cacheDir(), name), os.O_RDONLY|os.O_CREATE, 0o666)
	if err != nil {
		return nil
	}
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		f.Close()
		return nil
	}
	return f
}
