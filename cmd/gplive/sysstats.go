package main

import (
	"bufio"
	"math"
	"os"
	"strconv"
	"strings"
	"syscall"
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

// cpuLoadPercent is the 1 minute load average as a percentage of cores
// (truncated), matching the previous shell implementation.
func cpuLoadPercent() int {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0
	}
	load, _ := strconv.ParseFloat(strings.Fields(string(b))[0], 64)
	return int(load * 100 / float64(cpuCores()))
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
