package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type obj = map[string]any

var (
	reVolHost   = regexp.MustCompile(`^-v ([^:]+)`)
	reVolCont   = regexp.MustCompile(`[^:]+$`)
	reSvcType   = regexp.MustCompile(`^--link dokku\.([^.]+)`)
	reSvcName   = regexp.MustCompile(`([^.]+):[^:]+$`)
	rePortPair  = regexp.MustCompile(`\d+->\d+`)
	reCertName  = regexp.MustCompile(`^[^ ]+`)
	reCertDate  = regexp.MustCompile(`\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}`)
	rePluginVer = regexp.MustCompile(`^\S+\s+([0-9.]+)`)
)

func volumesFromOptions(options string) []obj {
	vols := []obj{}
	for _, line := range strings.Split(options, "\n") {
		if strings.HasPrefix(line, "-v ") {
			h, c := reVolHost.FindStringSubmatch(line), reVolCont.FindString(line)
			if h != nil {
				vols = append(vols, obj{"host": h[1], "container": c})
			}
		}
	}
	return vols
}

func servicesFromOptions(options string) []obj {
	svcs := []obj{}
	for _, line := range strings.Split(options, "\n") {
		if strings.HasPrefix(line, "--link dokku") {
			t, n := reSvcType.FindStringSubmatch(line), reSvcName.FindStringSubmatch(line)
			if t != nil && n != nil {
				svcs = append(svcs, obj{"type": t[1], "name": n[1]})
			}
		}
	}
	return svcs
}

// runFirst returns the output of the first command that succeeds.
func runFirst(cmds ...[]string) (string, error) {
	var err error
	for _, c := range cmds {
		var out []byte
		out, err = exec.Command(c[0], c[1:]...).Output()
		if err == nil {
			return strings.TrimSpace(string(out)), nil
		}
	}
	return "", err
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

func getAppNames() ([]string, error) {
	out, err := runFirst([]string{"dokku", "--quiet", "apps:list"}, []string{"dokku", "apps", "--quiet"})
	if err != nil {
		return nil, fmt.Errorf("listing apps: %w", err)
	}
	return nonEmptyLines(out), nil
}

func getCerts() map[string]string {
	certs := map[string]string{}
	out, err := runFirst([]string{"dokku", "--quiet", "letsencrypt:list"}, []string{"dokku", "letsencrypt:ls", "--quiet"})
	if err != nil {
		return certs
	}
	// rivergate-centre2016      2023-04-25 18:31:25       86d, 19h, 17m, 45s        56d, 19h, 17m, 45s
	for _, l := range nonEmptyLines(out) {
		if d := reCertDate.FindString(l); d != "" {
			certs[reCertName.FindString(l)] = d
		}
	}
	return certs
}

func getPlugins() ([]obj, error) {
	out, err := runFirst([]string{"dokku", "--quiet", "plugin:list"}, []string{"dokku", "plugin"})
	if err != nil {
		return nil, fmt.Errorf("listing plugins: %w", err)
	}
	plugins := []obj{}
	for _, l := range nonEmptyLines(out) {
		l = strings.TrimSpace(l)
		name := reCertName.FindString(l)
		m := rePluginVer.FindStringSubmatch(l)
		if m != nil && !strings.Contains(l, "dokku core") && name != "plugn:" {
			plugins = append(plugins, obj{"name": name, "version": m[1]})
		}
	}
	return plugins, nil
}

func dirEntries(dir string) []string {
	es, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range es {
		if !strings.HasPrefix(e.Name(), ".") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

type serviceSet struct {
	byKey map[string]obj
	order []obj
}

func getServices() *serviceSet {
	s := &serviceSet{byKey: map[string]obj{}}
	const base = "/var/lib/dokku/services/"
	for _, typ := range dirEntries(base) {
		for _, name := range dirEntries(base + typ) {
			svc := obj{"name": name, "type": typ, "state": "stopped", "exposed": ""}
			s.byKey["dokku."+typ+"."+name] = svc
			s.order = append(s.order, svc)
		}
	}
	return s
}

func readFile(path string) (string, bool) {
	b, err := os.ReadFile(path)
	return string(b), err == nil
}

func buildState(ctx context.Context) (obj, error) {
	var (
		wg        sync.WaitGroup
		appnames  []string
		appsErr   error
		plugins   []obj
		pluginErr error
		cs        []container
		csErr     error
	)
	wg.Add(3)
	go func() { defer wg.Done(); appnames, appsErr = getAppNames() }()
	go func() { defer wg.Done(); plugins, pluginErr = getPlugins() }()
	go func() { defer wg.Done(); cs, csErr = newDocker().containers(ctx, false) }()
	wg.Wait()
	if err := errors.Join(appsErr, pluginErr, csErr); err != nil {
		return nil, err
	}

	certs := map[string]string{}
	for _, p := range plugins {
		if p["name"] == "letsencrypt" {
			certs = getCerts()
		}
	}

	services := getServices()
	appContainers := map[string]*obj{}
	for _, c := range cs {
		name := c.name()
		switch {
		case strings.HasPrefix(c.Image, "dokku/") && !strings.HasSuffix(name, ".ambassador"):
			app := strings.TrimSuffix(strings.TrimPrefix(c.Image, "dokku/"), ":latest")
			if a, ok := appContainers[app]; ok {
				(*a)["processes"] = (*a)["processes"].(int) + 1
			} else {
				appContainers[app] = &obj{"container": c.shortID(), "processes": 1}
			}
		case services.byKey[name] != nil:
			svc := services.byKey[name]
			svc["status"] = "running"
			svc["container"] = c.shortID()
			svc["version"] = c.Image[strings.LastIndex(c.Image, ":")+1:]
		case strings.HasSuffix(name, ".ambassador") && services.byKey[strings.TrimSuffix(name, ".ambassador")] != nil:
			set := map[string]bool{}
			for _, p := range c.Ports {
				if p.PublicPort != 0 {
					set[strconv.Itoa(p.PublicPort)+"->"+strconv.Itoa(p.PrivatePort)] = true
				}
			}
			var pairs []string
			for k := range set {
				pairs = append(pairs, k)
			}
			sort.Strings(pairs)
			services.byKey[strings.TrimSuffix(name, ".ambassador")]["exposed"] = strings.Join(pairs, ",")
		}
	}

	apps := []obj{}
	root := dokkuRoot()
	for _, name := range appnames {
		app := obj{"name": name, "expires_at": nil}
		if ac, ok := appContainers[name]; ok {
			app["container"] = (*ac)["container"]
			app["status"] = "running"
			app["processes"] = (*ac)["processes"]
			if exp, ok := certs[name]; ok {
				app["expires_at"] = exp
			}
		} else {
			app["container"] = ""
			app["status"] = "stopped"
			app["processes"] = 0
		}
		if vhost, ok := readFile(filepath.Join(root, name, "VHOST")); ok {
			domains := []string{}
			for _, l := range strings.Split(strings.TrimSuffix(vhost, "\n"), "\n") {
				domains = append(domains, strings.TrimSpace(l))
			}
			if vhost == "" {
				domains = []string{}
			}
			app["domains"] = domains
		}
		opts := obj{}
		for _, stage := range []string{"build", "run", "deploy"} {
			content, ok := readFile(filepath.Join(root, name, "DOCKER_OPTIONS_"+strings.ToUpper(stage)))
			if !ok {
				continue
			}
			opts[stage] = content
			if stage == "deploy" {
				app["volumes"] = volumesFromOptions(content)
				app["services"] = servicesFromOptions(content)
			}
		}
		app["docker_options"] = opts
		apps = append(apps, app)
	}

	svcList := services.order
	if svcList == nil {
		svcList = []obj{}
	}
	return obj{
		"apps":     apps,
		"services": svcList,
		"plugins":  plugins,
		"memory":   approxMemMB(),
		"vcpus":    cpuCores(),
		"disk":     diskSizeMB(),
	}, nil
}

func marshalState(state obj, indent bool) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if indent {
		enc.SetIndent("", "    ")
	}
	enc.Encode(state)
	return buf.Bytes()
}

func getState(print bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	state, err := buildState(ctx)
	if err != nil {
		return err
	}
	if print {
		os.Stdout.Write(marshalState(state, true))
		return nil
	}
	os.WriteFile(filepath.Join(cacheDir(), "state.json"), marshalState(state, true), 0o644)
	postJSON(serverURLs(), "/duocms/api/dokku/state", marshalState(state, false), 2, 30*time.Second)
	return nil
}
