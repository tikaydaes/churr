package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// healthDefault is where `churr serve -behind-proxy` listens when -http is
// not given.
const healthDefault = "127.0.0.1:8472"

// health asks the server on this machine for /healthz over plain HTTP and
// returns nil when it answers 200. It is the container health check: the
// image has no shell and no curl.
func health(args []string) error {
	f := newFlags("churr health", "churr health [-http ADDRESS]")
	addr := f.text("http", healthDefault, "The -http address churr serve listens on. A missing or wildcard host means this machine.")
	rest, err := f.parse(args)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		f.Usage()
		return errors.New("churr health takes no arguments")
	}
	return checkHealth(*addr, 5*time.Second)
}

// checkHealth requests http://<addr>/healthz, with a wildcard or missing
// host taken as loopback.
func checkHealth(addr string, timeout time.Duration) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("-http %q is not host:port: %v", addr, err)
	}
	switch strings.Trim(host, "[]") {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	url := "http://" + net.JoinHostPort(host, port) + "/healthz"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("unhealthy: %v", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unhealthy: %s answered %s", url, resp.Status)
	}
	fmt.Println("ok")
	return nil
}
