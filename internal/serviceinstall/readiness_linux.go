//go:build linux

package serviceinstall

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/buildinfo"
	"golang.org/x/sys/unix"
)

var errUpgradeReadiness = errors.New("selected service process readiness is unverified")

type upgradeBuildIdentity struct {
	Version   string `json:"version"`
	Revision  string `json:"revision"`
	BuildTime string `json:"build_time"`
}
type upgradeProcessExpectation struct {
	UID                                uint32
	ImageSHA256, ConfigPath, HealthURL string
	Build                              upgradeBuildIdentity
	AllowIPv6Wildcard                  bool
}

// VerifySystemdUpgrade is read-only. It does not start a daemon, open SQLite,
// install/migrate data, signal processes, or use a non-loopback destination.
func VerifySystemdUpgrade(configPath string, pid int, expectedConfigSHA string) error {
	if pid < 1 || len(expectedConfigSHA) != 64 || strings.ToLower(expectedConfigSHA) != expectedConfigSHA {
		return errUpgradeReadiness
	}
	if _, err := hex.DecodeString(expectedConfigSHA); err != nil {
		return errUpgradeReadiness
	}
	uid, _, home, err := serviceIdentity()
	if err != nil {
		return errUpgradeReadiness
	}
	if !strings.HasPrefix(configPath, "/") || path.Clean(configPath) != configPath {
		return errUpgradeReadiness
	}
	root, err := syscall.Open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return errUpgradeReadiness
	}
	defer syscall.Close(root)
	raw, _, err := trustedConfig(root, strings.TrimPrefix(configPath, "/"), 0)
	if err != nil {
		return errUpgradeReadiness
	}
	cfg, err := parseConfig(raw)
	if err != nil {
		return errUpgradeReadiness
	}
	plan, err := PlanStorage(cfg, home)
	if err != nil || plan.HealthURL == "" {
		return errUpgradeReadiness
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != expectedConfigSHA {
		return errUpgradeReadiness
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	self, err := os.Open("/proc/self/exe")
	if err != nil {
		return errUpgradeReadiness
	}
	defer self.Close()
	image, err := upgradeImageHash(ctx, self)
	if err != nil {
		return errUpgradeReadiness
	}
	host, _, _ := net.SplitHostPort(cfg.Server.Listen)
	return verifyUpgradeProcess(ctx, pid, upgradeProcessExpectation{UID: uint32(uid), ImageSHA256: image, ConfigPath: configPath, HealthURL: plan.HealthURL, AllowIPv6Wildcard: host == "" || host == "0.0.0.0", Build: upgradeBuildIdentity{Version: buildinfo.Version, Revision: buildinfo.Revision, BuildTime: buildinfo.BuildTime}})
}

func upgradeImageHash(ctx context.Context, f *os.File) (string, error) {
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 512<<20 {
		return "", errUpgradeReadiness
	}
	h := sha256.New()
	buffer := make([]byte, 32<<10)
	var total int64
	for {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		n, e := f.Read(buffer)
		total += int64(n)
		if total > 512<<20 {
			return "", errUpgradeReadiness
		}
		if n > 0 {
			_, _ = h.Write(buffer[:n])
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return "", e
		}
	}
	if total != info.Size() {
		return "", errUpgradeReadiness
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func upgradeProcFile(dir int, name string, limit int64) ([]byte, error) {
	fd, err := unix.Openat(dir, name, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, errUpgradeReadiness
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(raw)) > limit {
		return nil, errUpgradeReadiness
	}
	return raw, nil
}

func upgradeProcessIdentity(dir int, expected upgradeProcessExpectation) (string, error) {
	status, err := upgradeProcFile(dir, "status", 64<<10)
	if err != nil {
		return "", err
	}
	uidFound := false
	for _, line := range strings.Split(string(status), "\n") {
		if !strings.HasPrefix(line, "Uid:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 5 {
			return "", errUpgradeReadiness
		}
		for _, value := range fields[1:] {
			uid, e := strconv.ParseUint(value, 10, 32)
			if e != nil || uint32(uid) != expected.UID {
				return "", errUpgradeReadiness
			}
		}
		uidFound = true
	}
	if !uidFound {
		return "", errUpgradeReadiness
	}
	stat, err := upgradeProcFile(dir, "stat", 64<<10)
	if err != nil {
		return "", err
	}
	end := strings.LastIndex(string(stat), ") ")
	if end < 0 {
		return "", errUpgradeReadiness
	}
	fields := strings.Fields(string(stat)[end+2:])
	if len(fields) < 20 || fields[0] == "Z" || fields[0] == "X" {
		return "", errUpgradeReadiness
	}
	if ticks, e := strconv.ParseUint(fields[19], 10, 64); e != nil || ticks == 0 {
		return "", errUpgradeReadiness
	}
	args, err := upgradeProcFile(dir, "cmdline", 64<<10)
	if err != nil {
		return "", err
	}
	argv := strings.Split(strings.TrimSuffix(string(args), "\x00"), "\x00")
	valid := len(argv) == 3 && (argv[1] == "-config" || argv[1] == "--config") && argv[2] == expected.ConfigPath
	if len(argv) == 2 {
		valid = (argv[1] == "-config="+expected.ConfigPath || argv[1] == "--config="+expected.ConfigPath)
	}
	if !valid {
		return "", errUpgradeReadiness
	}
	return fields[19], nil
}

// Linux documents the PID-directory descriptor lifetime and TCP inode fields:
// https://docs.kernel.org/filesystems/proc.html
// https://docs.kernel.org/networking/proc_net_tcp.html
// All subsequent reads stay relative to the opened PID directory, so PID reuse
// cannot redirect the observation to a replacement process.
func upgradeOwnedListener(dir int, wanted net.IP, port int) (string, error) {
	fd, err := unix.Openat(dir, "fd", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", errUpgradeReadiness
	}
	f := os.NewFile(uintptr(fd), "service-fd")
	defer f.Close()
	entries, err := f.Readdirnames(4097)
	if err != nil && err != io.EOF || len(entries) > 4096 {
		return "", errUpgradeReadiness
	}
	sockets := map[string]bool{}
	for _, name := range entries {
		if _, e := strconv.ParseUint(name, 10, 32); e != nil {
			return "", errUpgradeReadiness
		}
		buffer := make([]byte, 4096)
		n, e := unix.Readlinkat(fd, name, buffer)
		if e != nil {
			continue
		}
		target := string(buffer[:n])
		if strings.HasPrefix(target, "socket:[") && strings.HasSuffix(target, "]") {
			inode := strings.TrimSuffix(strings.TrimPrefix(target, "socket:["), "]")
			if value, e := strconv.ParseUint(inode, 10, 64); e == nil && value > 0 {
				sockets[inode] = true
			}
		}
	}
	table := "net/tcp"
	if wanted.To4() == nil {
		table = "net/tcp6"
	}
	dataFD, err := unix.Openat(dir, table, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", errUpgradeReadiness
	}
	data := os.NewFile(uintptr(dataFD), table)
	defer data.Close()
	limited := io.LimitReader(data, 8<<20)
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 4096), 64<<10)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 10 || fields[3] != "0A" || !sockets[fields[9]] {
			continue
		}
		address, rawPort, ok := strings.Cut(fields[1], ":")
		if !ok || len(rawPort) != 4 {
			continue
		}
		p, e := strconv.ParseUint(rawPort, 16, 16)
		if e != nil || int(p) != port {
			continue
		}
		ip, e := upgradeProcIP(address)
		if e != nil {
			continue
		}
		if ip.IsUnspecified() || ip.Equal(wanted) {
			return fields[9], nil
		}
	}
	return "", errUpgradeReadiness
}

func upgradeProcIP(value string) (net.IP, error) {
	if len(value) != 8 && len(value) != 32 {
		return nil, errUpgradeReadiness
	}
	ip := make(net.IP, len(value)/2)
	for i := 0; i < len(value); i += 8 {
		part, err := strconv.ParseUint(value[i:i+8], 16, 32)
		if err != nil {
			return nil, errUpgradeReadiness
		}
		binary.NativeEndian.PutUint32(ip[i/2:i/2+4], uint32(part))
	}
	return ip, nil
}

func upgradeSameNetworkNamespace(dir int) bool {
	buffer := make([]byte, 128)
	n, err := unix.Readlinkat(dir, "ns/net", buffer)
	if err != nil || n >= len(buffer) {
		return false
	}
	self, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		return false
	}
	return strings.HasPrefix(self, "net:[") && self == string(buffer[:n])
}

func verifyUpgradeProcess(ctx context.Context, pid int, expected upgradeProcessExpectation) error {
	if pid < 1 || expected.UID == 0 || expected.ConfigPath == "" || len(expected.ImageSHA256) != 64 {
		return errUpgradeReadiness
	}
	u, err := url.Parse(expected.HealthURL)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Path != "/v1/health" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return errUpgradeReadiness
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return errUpgradeReadiness
	}
	var targets []net.IP
	if u.Hostname() == "localhost" {
		targets = []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback}
	} else {
		ip := net.ParseIP(u.Hostname())
		if ip == nil || !ip.IsLoopback() {
			return errUpgradeReadiness
		}
		targets = []net.IP{ip}
		if ip.To4() != nil && expected.AllowIPv6Wildcard {
			targets = append(targets, net.IPv6loopback)
		}
	}
	dir, err := unix.Open(fmt.Sprintf("/proc/%d", pid), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return errUpgradeReadiness
	}
	defer unix.Close(dir)
	if !upgradeSameNetworkNamespace(dir) {
		return errUpgradeReadiness
	}
	identity, err := upgradeProcessIdentity(dir, expected)
	if err != nil {
		return err
	}
	checkImage := func() error {
		fd, e := unix.Openat(dir, "exe", unix.O_RDONLY|unix.O_CLOEXEC, 0)
		if e != nil {
			return errUpgradeReadiness
		}
		f := os.NewFile(uintptr(fd), "service-image")
		defer f.Close()
		hash, e := upgradeImageHash(ctx, f)
		if e != nil || hash != expected.ImageSHA256 {
			return errUpgradeReadiness
		}
		return nil
	}
	if err = checkImage(); err != nil {
		return err
	}
	var chosen net.IP
	var inode string
	for _, candidate := range targets {
		inode, err = upgradeOwnedListener(dir, candidate, port)
		if err == nil {
			chosen = candidate
			break
		}
	}
	if chosen == nil {
		return errUpgradeReadiness
	}
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	get := func(route string, out any) error {
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+net.JoinHostPort(chosen.String(), strconv.Itoa(port))+route, nil)
		if e != nil {
			return errUpgradeReadiness
		}
		response, e := client.Do(req)
		if e != nil {
			return errUpgradeReadiness
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			return errUpgradeReadiness
		}
		body, e := io.ReadAll(io.LimitReader(response.Body, (8<<10)+1))
		if e != nil || len(body) > 8<<10 || json.Unmarshal(body, out) != nil {
			return errUpgradeReadiness
		}
		return nil
	}
	var health struct {
		Status string `json:"status"`
	}
	if get("/v1/health", &health) != nil || health.Status != "ok" {
		return errUpgradeReadiness
	}
	var about upgradeBuildIdentity
	if get("/v1/about", &about) != nil || about != expected.Build {
		return errUpgradeReadiness
	}
	after, err := upgradeProcessIdentity(dir, expected)
	if err != nil || after != identity {
		return errUpgradeReadiness
	}
	retained, err := upgradeOwnedListener(dir, chosen, port)
	if err != nil || retained != inode || !upgradeSameNetworkNamespace(dir) {
		return errUpgradeReadiness
	}
	return checkImage()
}
