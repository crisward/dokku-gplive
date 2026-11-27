package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

type containerStat struct {
	CPU pct `json:"cpu"`
	Mem pct `json:"mem"`
	Dsk pct `json:"dsk"`
}

type statsPayload struct {
	CPU        pct                      `json:"cpu"`
	Mem        pct                      `json:"mem"`
	Dsk        pct                      `json:"dsk"`
	Containers map[string]containerStat `json:"containers"`
}

func collectStats() statsPayload {
	p := statsPayload{
		CPU:        cpuUsedPercent(),
		Mem:        memUsedPercent(),
		Dsk:        rootDiskUsedPercent(),
		Containers: map[string]containerStat{},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	d := newDocker()
	cs, err := d.containers(ctx, false)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gplive: docker:", err)
		return p
	}

	cache := loadDiskCache()
	if cache.needsRefresh(cs) {
		spawnDiskRefresh()
	}
	totalKB := rootDiskTotalKB()

	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, c := range cs {
		wg.Add(1)
		go func(c container) {
			defer wg.Done()
			cpu, mem, err := d.stats(ctx, c.ID)
			if err != nil {
				return
			}
			s := containerStat{CPU: pct(cpu), Mem: pct(mem)}
			if totalKB > 0 {
				s.Dsk = pct(float64(cache.Containers[c.shortID()].Bytes/1024) / float64(totalKB) * 100)
			}
			mu.Lock()
			p.Containers[c.shortID()] = s
			mu.Unlock()
		}(c)
	}
	wg.Wait()
	return p
}

func sendStats() error {
	lock := tryLock("stats.lock")
	if lock == nil {
		return nil // previous run still going
	}
	defer lock.Close()
	body, _ := json.Marshal(collectStats())
	postJSON(serverURLs(), "/duocms/api/servers/stats", body, 0, 30*time.Second)
	return nil
}
