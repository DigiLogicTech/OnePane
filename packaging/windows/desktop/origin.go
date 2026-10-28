package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Allocate once per Windows profile, then bind that same port on every launch.
// A busy or corrupt saved port is an explicit startup error, never a new empty
// browser storage origin. The desktop's existing single-instance guard owns it.
func listenDesktopOrigin(path string) (net.Listener, error) {
	raw, err := os.ReadFile(path)
	if err == nil {
		port, parseErr := strconv.Atoi(strings.TrimSpace(string(raw)))
		if parseErr != nil || port < 1024 || port > 65535 {
			return nil, fmt.Errorf("invalid saved desktop port in %s", path)
		}
		ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			return nil, fmt.Errorf("desktop port %d is unavailable; close the application using it and retry: %w", port, err)
		}
		return ln, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	address := "127.0.0.1:0"
	// On the first fixed build, retain the most recently used legacy origin.
	// Read only bounded diagnostic text; never inspect WebView credential data.
	if log, err := os.Open(filepath.Join(filepath.Dir(path), "desktop.log")); err == nil {
		if info, err := log.Stat(); err == nil {
			start := info.Size() - 128*1024
			if start < 0 {
				start = 0
			}
			data := make([]byte, info.Size()-start)
			if _, err := log.ReadAt(data, start); err == nil {
				matches := regexp.MustCompile(`desktop UI gateway listening at http://127\.0\.0\.1:([0-9]{1,5})`).FindAllSubmatch(data, -1)
				if len(matches) > 0 {
					port, _ := strconv.Atoi(string(matches[len(matches)-1][1]))
					if port >= 1024 && port <= 65535 {
						address = net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
					}
				}
			}
		}
		log.Close()
	}
	ln, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		ln.Close()
		return nil, err
	}
	_, writeErr := f.WriteString(port + "\n")
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		ln.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("save desktop origin: write=%v close=%v", writeErr, closeErr)
	}
	return ln, nil
}
