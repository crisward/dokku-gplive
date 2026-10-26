package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestOptionParsing(t *testing.T) {
	opts := "-v /a/b:/data\n--link dokku.postgres.db1:db1\n--link dokku.redis.cache:cache\n"
	if v := volumesFromOptions(opts); !reflect.DeepEqual(v, []obj{{"host": "/a/b", "container": "/data"}}) {
		t.Errorf("volumes: %v", v)
	}
	want := []obj{{"type": "postgres", "name": "db1"}, {"type": "redis", "name": "cache"}}
	if s := servicesFromOptions(opts); !reflect.DeepEqual(s, want) {
		t.Errorf("services: %v", s)
	}
}

func TestSkipMountAndNesting(t *testing.T) {
	for _, p := range []string{"/", "/var", "/var/lib/docker", "/proc", "/etc/ssl", "/run/x"} {
		if !skipMount(p) {
			t.Errorf("%s should be skipped", p)
		}
	}
	for _, p := range []string{"/var/lib/dokku/services/postgres/db/data", "/var/lib/docker/volumes/v/_data", "/home/dokku/app/storage"} {
		if skipMount(p) {
			t.Errorf("%s should not be skipped", p)
		}
	}
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "a", "b"), 0o755)
	got := pathsToMeasure([]mount{{Source: filepath.Join(dir, "a", "b")}, {Source: filepath.Join(dir, "a")}})
	if len(got) != 1 || got[0] != filepath.Join(dir, "a") {
		t.Errorf("nested mounts not collapsed: %v", got)
	}
}

func TestDirSizeCountsHardlinksOnce(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "f")
	os.WriteFile(f, make([]byte, 1<<20), 0o644)
	one, _ := dirSize(context.Background(), dir)
	os.Link(f, filepath.Join(dir, "g"))
	two, _ := dirSize(context.Background(), dir)
	if one < 1<<20 {
		t.Errorf("size too small: %d", one)
	}
	if two-one > 8192 { // only the extra directory entry may differ
		t.Errorf("hardlink double counted: %d vs %d", one, two)
	}
}
