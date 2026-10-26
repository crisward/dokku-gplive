package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

// ipv4Client mirrors curl --ipv4.
func ipv4Client(timeout time.Duration) *http.Client {
	d := &net.Dialer{Timeout: 10 * time.Second}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
				return d.DialContext(ctx, "tcp4", addr)
			},
		},
	}
}

// postJSON sends body to every server in parallel, retrying failures.
func postJSON(urls []string, path string, body []byte, retries int, timeout time.Duration) {
	client := ipv4Client(timeout)
	var wg sync.WaitGroup
	for _, u := range urls {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			var err error
			for attempt := 0; attempt <= retries; attempt++ {
				if attempt > 0 {
					time.Sleep(time.Duration(attempt) * 2 * time.Second)
				}
				if err = postOnce(client, u+path, body); err == nil {
					return
				}
			}
			fmt.Fprintf(os.Stderr, "gplive: post %s%s: %v\n", u, path, err)
		}(u)
	}
	wg.Wait()
}

func postOnce(c *http.Client, url string, body []byte) error {
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 500 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}
