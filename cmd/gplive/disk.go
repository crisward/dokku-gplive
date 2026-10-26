package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Disk usage is expensive to measure, so it is measured by a separate
// low-priority `gplive disk-refresh` process and cached. The per-minute stats
// run only reads the cache.

const (
	diskTTL         = 10 * time.Minute
	diskPathTimeout = 3 * time.Minute
)

type diskEntry struct {
	Bytes int64 `json:"bytes"`
	At    int64 `json:"at"`
}

type diskCache struct {
	Containers map[string]diskEntry `json:"containers"` // by short container id
	Paths      map[string]diskEntry `json:"paths"`
}

func diskCachePath() string { return filepath.Join(cacheDir(), "disk.json") }

func loadDiskCache() diskCache {
	c := diskCache{Containers: map[string]diskEntry{}, Paths: map[string]diskEntry{}}
	if b, err := os.ReadFile(diskCachePath()); err == nil {
		json.Unmarshal(b, &c)
	}
	if c.Containers == nil {
		c.Containers = map[string]diskEntry{}
	}
	if c.Paths == nil {
		c.Paths = map[string]diskEntry{}
	}
	return c
}

func (c diskCache) save() error {
	b, _ := json.Marshal(c)
	tmp := diskCachePath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, diskCachePath())
}

// needsRefresh reports whether any running container has no fresh entry.
func (c diskCache) needsRefresh(running []container) bool {
	for _, ct := range running {
		e, ok := c.Containers[ct.shortID()]
		if !ok || time.Since(time.Unix(e.At, 0)) > diskTTL {
			return true
		}
	}
	return false
}

// spawnDiskRefresh starts a detached refresh; it exits immediately if one is
// already running.
func spawnDiskRefresh() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(exe, "disk-refresh")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if cmd.Start() == nil {
		go cmd.Wait()
		cmd.Process.Release()
	}
}

// pathsToMeasure returns the directory mount sources of a container, skipping
// mounts that would count the whole host and nested duplicates.
func pathsToMeasure(mounts []mount) []string {
	var paths []string
	for _, m := range mounts {
		if m.Source == "" || skipMount(m.Source) {
			continue
		}
		if fi, err := os.Stat(m.Source); err != nil || !fi.IsDir() {
			continue
		}
		paths = append(paths, filepath.Clean(m.Source))
	}
	sort.Strings(paths)
	var out []string
	for _, p := range paths {
		if len(out) > 0 && isWithin(p, out[len(out)-1]) {
			continue
		}
		out = append(out, p)
	}
	return out
}

func isWithin(p, parent string) bool {
	return p == parent || strings.HasPrefix(p, strings.TrimSuffix(parent, "/")+"/")
}

func skipMount(p string) bool {
	p = filepath.Clean(p)
	// any mount that contains docker's own storage would count every container
	if isWithin("/var/lib/docker", p) {
		return true
	}
	for _, pre := range []string{"/proc", "/sys", "/dev", "/run", "/var/run", "/etc"} {
		if isWithin(p, pre) {
			return true
		}
	}
	return false
}

// dirSize is du-like: allocated bytes, not crossing devices, hardlinks counted once.
func dirSize(ctx context.Context, root string) (int64, error) {
	var rootStat syscall.Stat_t
	if err := syscall.Lstat(root, &rootStat); err != nil {
		return 0, err
	}
	type key struct{ dev, ino uint64 }
	seen := map[key]bool{}
	var total int64
	var walk func(dir string) error
	walk = func(dir string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		f, err := os.Open(dir)
		if err != nil {
			return nil // unreadable, count what we can
		}
		names, _ := f.Readdirnames(-1)
		f.Close()
		for _, n := range names {
			p := filepath.Join(dir, n)
			var st syscall.Stat_t
			if syscall.Lstat(p, &st) != nil {
				continue
			}
			if uint64(st.Dev) != uint64(rootStat.Dev) {
				continue
			}
			isDir := st.Mode&syscall.S_IFMT == syscall.S_IFDIR
			if !isDir && st.Nlink > 1 {
				k := key{uint64(st.Dev), uint64(st.Ino)}
				if seen[k] {
					continue
				}
				seen[k] = true
			}
			total += st.Blocks * 512
			if isDir {
				if err := walk(p); err != nil {
					return err
				}
			}
		}
		return nil
	}
	total += rootStat.Blocks * 512
	err := walk(root)
	return total, err
}

// lowPriority makes the refresh yield to the real workloads.
func lowPriority() {
	syscall.Setpriority(syscall.PRIO_PROCESS, 0, 19)
	nr := map[string]uintptr{"amd64": 251, "arm64": 30}[runtime.GOARCH]
	if nr != 0 {
		// ioprio_set(IOPRIO_WHO_PROCESS, self, IDLE class)
		syscall.Syscall(nr, 1, 0, 3<<13)
	}
}

func diskRefresh() error {
	lock := tryLock("disk.lock")
	if lock == nil {
		return nil
	}
	defer lock.Close()
	lowPriority()

	ctx := context.Background()
	cs, err := newDocker().containers(ctx, true)
	if err != nil {
		return err
	}
	old := loadDiskCache()
	next := diskCache{Containers: map[string]diskEntry{}, Paths: map[string]diskEntry{}}
	now := time.Now().Unix()

	perContainer := map[string][]string{}
	unique := map[string]bool{}
	for _, c := range cs {
		ps := pathsToMeasure(c.Mounts)
		perContainer[c.shortID()] = ps
		for _, p := range ps {
			unique[p] = true
		}
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 2)
	for p := range unique {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			pctx, cancel := context.WithTimeout(ctx, diskPathTimeout)
			defer cancel()
			size, err := dirSize(pctx, p)
			mu.Lock()
			defer mu.Unlock()
			if err != nil { // timed out: keep the previous measurement
				if e, ok := old.Paths[p]; ok {
					next.Paths[p] = e
				}
				return
			}
			next.Paths[p] = diskEntry{size, now}
		}(p)
	}
	wg.Wait()

	for _, c := range cs {
		total := c.SizeRw
		for _, p := range perContainer[c.shortID()] {
			total += next.Paths[p].Bytes
		}
		next.Containers[c.shortID()] = diskEntry{total, now}
	}
	return next.save()
}
