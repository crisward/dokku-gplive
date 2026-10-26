package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

type dockerClient struct{ http *http.Client }

func newDocker() *dockerClient {
	sock := os.Getenv("DOCKER_SOCK")
	if sock == "" {
		sock = "/var/run/docker.sock"
	}
	return &dockerClient{http: &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		},
	}}}
}

func (d *dockerClient) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", "http://docker"+path, nil)
	if err != nil {
		return err
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("docker %s: status %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

type port struct {
	PrivatePort int
	PublicPort  int
}

type mount struct {
	Type   string
	Source string
}

type container struct {
	ID     string
	Names  []string
	Image  string
	Ports  []port
	Mounts []mount
	SizeRw int64
}

func (c container) name() string {
	if len(c.Names) == 0 {
		return ""
	}
	return strings.TrimPrefix(c.Names[0], "/")
}

func (c container) shortID() string {
	if len(c.ID) > 12 {
		return c.ID[:12]
	}
	return c.ID
}

func (d *dockerClient) containers(ctx context.Context, withSize bool) ([]container, error) {
	path := "/containers/json"
	if withSize {
		path += "?size=true"
	}
	var cs []container
	return cs, d.get(ctx, path, &cs)
}

type statsResp struct {
	CPU    cpuStats `json:"cpu_stats"`
	PreCPU cpuStats `json:"precpu_stats"`
	Memory struct {
		Usage int64
		Limit int64
		Stats map[string]int64
	} `json:"memory_stats"`
}

type cpuStats struct {
	CPUUsage struct {
		Total  uint64   `json:"total_usage"`
		PerCPU []uint64 `json:"percpu_usage"`
	} `json:"cpu_usage"`
	System uint64 `json:"system_cpu_usage"`
	Online int    `json:"online_cpus"`
}

// stats returns cpu and memory percentages the same way `docker stats` does.
func (d *dockerClient) stats(ctx context.Context, id string) (cpu, mem float64, err error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var s statsResp
	// stream=false makes the daemon take two samples so precpu_stats is populated
	if err = d.get(ctx, "/containers/"+id+"/stats?stream=false", &s); err != nil {
		return
	}
	cpuDelta := float64(s.CPU.CPUUsage.Total) - float64(s.PreCPU.CPUUsage.Total)
	sysDelta := float64(s.CPU.System) - float64(s.PreCPU.System)
	online := float64(s.CPU.Online)
	if online == 0 {
		online = float64(len(s.CPU.CPUUsage.PerCPU))
	}
	if sysDelta > 0 && cpuDelta > 0 {
		cpu = cpuDelta / sysDelta * online * 100
	}
	used := s.Memory.Usage
	if v, ok := s.Memory.Stats["inactive_file"]; ok { // cgroup v2
		used -= v
	} else if v, ok := s.Memory.Stats["total_inactive_file"]; ok { // cgroup v1
		used -= v
	}
	if s.Memory.Limit > 0 && used > 0 {
		mem = float64(used) / float64(s.Memory.Limit) * 100
	}
	return
}
