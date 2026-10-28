package main

import (
	"fmt"
	"net/url"
	"strings"
	"syscall"
	"unsafe"
)

func openExternalURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 8192 || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || strings.ContainsAny(raw, "\r\n\x00") {
		return fmt.Errorf("external address must be an HTTPS website")
	}
	verb, _ := syscall.UTF16PtrFromString("open")
	target, err := syscall.UTF16PtrFromString(u.String())
	if err != nil {
		return err
	}
	ret, _, _ := shell32.NewProc("ShellExecuteW").Call(mainHwnd, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(target)), 0, 0, swShow)
	if ret <= 32 {
		return fmt.Errorf("system browser returned %d", ret)
	}
	return nil
}
