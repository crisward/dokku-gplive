package main

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func cpuCores() int {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return 1
	}
	defer f.Close()
	n := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "processor") {
			n++
		}
	}
	if n == 0 {
		return 1
	}
	return n
}

// cpuSample is a snapshot of the aggregate "cpu" line of /proc/stat.
type cpuSample struct {
	Total uint64 `json:"total"`
	Idle  uint64 `json:"idle"` // idle + iowait
	At    int64  `json:"at"`   // unix seconds
}

func readCPUSample() (cpuSample, bool) {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return cpuSample{}, false
	}
	line, _, _ := strings.Cut(string(b), "\n")
	fs := strings.Fields(line)
	if len(fs) < 5 || fs[0] != "cpu" {
		return cpuSample{}, false
	}
	var s cpuSample
	for i, f := range fs[1:] {
		v, _ := strconv.ParseUint(f, 10, 64)
		// guest and guest_nice (fields 9, 10) are already counted in user/nice
		if i >= 8 {
			break
		}
		s.Total += v
		if i == 3 || i == 4 { // idle, iowait
			s.Idle += v
		}
	}
	s.At = time.Now().Unix()
	return s, true
}

// maxCPUSampleAge is how old the saved sample may be and still be used as the
// baseline; beyond that we take a fresh short sample instead.
const maxCPUSampleAge = 10 * 60

// cpuUsedPercent is real CPU utilisation (0-100, all cores combined), measured
// from /proc/stat deltas. It diffs against the sample saved by the previous
// run, falling back to a short in-process sample when there is none.
func cpuUsedPercent() int {
	cur, ok := readCPUSample()
	if !ok {
		return 0
	}
	path := filepath.Join(cacheDir(), "cpu.stat")
	var prev cpuSample
	have := false
	if b, err := os.ReadFile(path); err == nil && json.Unmarshal(b, &prev) == nil {
		have = prev.Total > 0 && cur.At-prev.At >= 0 && cur.At-prev.At <= maxCPUSampleAge && cur.Total > prev.Total
	}
	if !have {
		prev = cur
		time.Sleep(500 * time.Millisecond)
		if cur, ok = readCPUSample(); !ok {
			return 0
		}
	}
	if b, err := json.Marshal(cur); err == nil {
		os.WriteFile(path, b, 0o644)
	}
	return cpuPercent(prev, cur)
}

func cpuPercent(prev, cur cpuSample) int {
	if cur.Total <= prev.Total || cur.Idle < prev.Idle {
		return 0
	}
	total := float64(cur.Total - prev.Total)
	idle := float64(cur.Idle - prev.Idle)
	pct := (1 - idle/total) * 100
	return int(math.Min(100, math.Max(0, math.Round(pct))))
}

func memInfo() (total, available float64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) < 2 {
			continue
		}
		v, _ := strconv.ParseFloat(fs[1], 64)
		switch fs[0] {
		case "MemTotal:":
			total = v
		case "MemAvailable:":
			available = v
		}
	}
	return
}

// float6 marshals like awk's default number output (6 significant digits).
type float6 float64

func (f float6) MarshalJSON() ([]byte, error) {
	return []byte(strconv.FormatFloat(float64(f), 'g', 6, 64)), nil
}

func memUsedPercent() float6 {
	total, avail := memInfo()
	if total == 0 {
		return 0
	}
	return float6(100 - avail/total*100)
}

// rootDiskUsedPercent matches the Use% column of `df /`.
func rootDiskUsedPercent() int {
	var s syscall.Statfs_t
	if syscall.Statfs("/", &s) != nil {
		return 0
	}
	used := float64(s.Blocks - s.Bfree)
	avail := float64(s.Bavail)
	if used+avail == 0 {
		return 0
	}
	return int(math.Ceil(used * 100 / (used + avail)))
}

func rootDiskTotalKB() int64 {
	var s syscall.Statfs_t
	if syscall.Statfs("/", &s) != nil {
		return 0
	}
	return int64(s.Blocks) * int64(s.Frsize) / 1024
}

// approxMemMB is physical memory rounded up to the next whole GB, in MB.
func approxMemMB() int {
	kb := float64(os.Getpagesize()) * float64(physPages()) / 1024
	return int(math.Ceil(math.Ceil(kb/1024)/1024) * 1024)
}

func physPages() int64 {
	var si syscall.Sysinfo_t
	if syscall.Sysinfo(&si) != nil {
		return 0
	}
	return int64(si.Totalram) * int64(si.Unit) / int64(os.Getpagesize())
}

// diskSizeMB is the size of / in (decimal) MB, rounded up.
func diskSizeMB() int {
	var s syscall.Statfs_t
	if syscall.Statfs("/", &s) != nil {
		return 0
	}
	total := float64(s.Blocks) * float64(s.Frsize)
	return int(math.Ceil(math.Ceil(total/1000) / 1000))
}
