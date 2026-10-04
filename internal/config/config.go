package config

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server     ServerConfig     `yaml:"server"`
	Storage    StorageConfig    `yaml:"storage"`
	Logging    LoggingConfig    `yaml:"logging"`
	LocalAI    LocalAIConfig    `yaml:"local_ai"`
	Federation FederationConfig `yaml:"node_federation"`
}

type ServerConfig struct {
	Listen              string `yaml:"listen"`
	PublicOrigin        string `yaml:"public_origin,omitempty"`
	PreviewListen       string `yaml:"preview_listen"`
	PreviewPublicOrigin string `yaml:"preview_public_origin,omitempty"`
}

type StorageConfig struct {
	DataDir     string `yaml:"data_dir"`
	ProjectRoot string `yaml:"project_root,omitempty"`
}
type LoggingConfig struct {
	Level string `yaml:"level"`
}

type FederationConfig struct {
	Enabled            bool   `yaml:"enabled"`
	Listen             string `yaml:"listen"`
	AdvertiseURL       string `yaml:"advertise_url,omitempty"`
	DiscoveryEnabled   bool   `yaml:"discovery_enabled"`
	DiscoveryMulticast string `yaml:"discovery_multicast"`
	HeartbeatSeconds   int    `yaml:"heartbeat_seconds"`
	StaleSeconds       int    `yaml:"stale_seconds"`
}

type LocalAIConfig struct {
	CatalogURL           string            `yaml:"catalog_url,omitempty"`
	CatalogTrustKeys     map[string]string `yaml:"catalog_trust_keys,omitempty"`
	IdleUnloadMinutes    int               `yaml:"idle_unload_minutes,omitempty"`
	ModelPoolPath        string            `yaml:"model_pool_path,omitempty"`
	ResidencyHeadroomPct int               `yaml:"residency_headroom_pct,omitempty"`
	LLMFitURL            string            `yaml:"llmfit_url,omitempty"`
}

func Default() Config {
	return Config{Server: ServerConfig{Listen: "127.0.0.1:8080", PreviewListen: "127.0.0.1:8081"}, Storage: StorageConfig{DataDir: "/var/lib/onepane"}, Logging: LoggingConfig{Level: "info"}, LocalAI: LocalAIConfig{IdleUnloadMinutes: 15, ResidencyHeadroomPct: 10}, Federation: FederationConfig{Enabled: true, Listen: "0.0.0.0:18443", DiscoveryEnabled: true, DiscoveryMulticast: "239.255.77.77:47777", HeartbeatSeconds: 10, StaleSeconds: 35}}
}

func Load(path string) (Config, error) {
	cfg := Default()
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return Config{}, fmt.Errorf("read config: %w", err)
		}
		if err := yaml.Unmarshal(b, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse config: %w", err)
		}
	}
	if err := Validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func Validate(cfg Config) error {
	if strings.TrimSpace(cfg.Server.Listen) == "" {
		return fmt.Errorf("server.listen must not be empty")
	}
	if strings.TrimSpace(cfg.Server.PreviewListen) == "" {
		return fmt.Errorf("server.preview_listen must not be empty")
	}
	if cfg.Server.PreviewListen == cfg.Server.Listen {
		return fmt.Errorf("server.preview_listen must use a different address/port from server.listen")
	}
	if strings.TrimSpace(cfg.Storage.DataDir) == "" {
		return fmt.Errorf("storage.data_dir must not be empty")
	}
	if v := strings.TrimSpace(cfg.Storage.ProjectRoot); v != "" && !filepath.IsAbs(v) {
		return fmt.Errorf("storage.project_root must be an absolute path")
	}
	host, _, err := net.SplitHostPort(cfg.Server.Listen)
	if err != nil {
		return fmt.Errorf("server.listen must be host:port: %w", err)
	}
	apiLoopback := isLoopbackHost(host)
	if !apiLoopback {
		if _, err := requireHTTPSOrigin(cfg.Server.PublicOrigin, "non-loopback server.listen requires https server.public_origin"); err != nil {
			return err
		}
	}
	previewHost, _, err := net.SplitHostPort(cfg.Server.PreviewListen)
	if err != nil {
		return fmt.Errorf("server.preview_listen must be host:port: %w", err)
	}
	previewLoopback := isLoopbackHost(previewHost)
	if !previewLoopback {
		if _, err := requireHTTPSOrigin(cfg.Server.PreviewPublicOrigin, "non-loopback server.preview_listen requires a valid https server.preview_public_origin"); err != nil {
			return err
		}
	}
	// Once the control plane is remotely exposed, preview must also have an
	// explicit TLS origin. The trusted reverse proxy may still forward that
	// origin to a loopback-only preview listener.
	if !apiLoopback || strings.TrimSpace(cfg.Server.PublicOrigin) != "" {
		if _, err := requireHTTPSOrigin(cfg.Server.PreviewPublicOrigin, "remote server.public_origin requires a valid https server.preview_public_origin"); err != nil {
			return err
		}
	}
	if normalizeOrigin(APIOrigin(cfg)) == normalizeOrigin(PreviewOrigin(cfg)) {
		return fmt.Errorf("preview origin must be distinct from the control-plane origin")
	}
	for keyID, encoded := range cfg.LocalAI.CatalogTrustKeys {
		if strings.TrimSpace(keyID) == "" {
			return fmt.Errorf("local_ai.catalog_trust_keys contains an empty key id")
		}
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
		if err != nil || len(raw) != 32 {
			return fmt.Errorf("local_ai.catalog_trust_keys[%s] must be a base64 Ed25519 public key", keyID)
		}
	}
	if v := strings.TrimSpace(cfg.LocalAI.CatalogURL); v != "" {
		u, err := url.Parse(v)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return fmt.Errorf("local_ai.catalog_url must be an https URL")
		}
	}
	if cfg.LocalAI.IdleUnloadMinutes < 0 || cfg.LocalAI.IdleUnloadMinutes > 1440 {
		return fmt.Errorf("local_ai.idle_unload_minutes must be between 0 and 1440")
	}
	if v := strings.TrimSpace(cfg.LocalAI.ModelPoolPath); v != "" && !filepath.IsAbs(v) {
		return fmt.Errorf("local_ai.model_pool_path must be an absolute path")
	}
	if cfg.LocalAI.ResidencyHeadroomPct < 0 || cfg.LocalAI.ResidencyHeadroomPct > 50 {
		return fmt.Errorf("local_ai.residency_headroom_pct must be between 0 and 50")
	}
	if v := strings.TrimSpace(cfg.LocalAI.LLMFitURL); v != "" {
		u, err := url.Parse(v)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("local_ai.llmfit_url must be a clean http(s) base URL")
		}
		if u.Scheme == "http" && !isLoopbackHost(u.Hostname()) {
			return fmt.Errorf("plaintext local_ai.llmfit_url is permitted only on loopback")
		}
	}
	if cfg.Federation.Enabled {
		if strings.TrimSpace(cfg.Federation.Listen) == "" {
			return fmt.Errorf("node_federation.listen must not be empty")
		}
		if _, _, err := net.SplitHostPort(cfg.Federation.Listen); err != nil {
			return fmt.Errorf("node_federation.listen must be host:port: %w", err)
		}
		if v := strings.TrimSpace(cfg.Federation.AdvertiseURL); v != "" {
			u, err := url.Parse(v)
			if err != nil || u.Scheme != "https" || u.Host == "" {
				return fmt.Errorf("node_federation.advertise_url must be an https URL")
			}
		}
		if cfg.Federation.DiscoveryEnabled {
			a, err := net.ResolveUDPAddr("udp4", cfg.Federation.DiscoveryMulticast)
			if err != nil || a.IP == nil || !a.IP.IsMulticast() || a.Port < 1 {
				return fmt.Errorf("node_federation.discovery_multicast must be an IPv4 multicast host:port")
			}
		}
		if cfg.Federation.HeartbeatSeconds < 2 || cfg.Federation.HeartbeatSeconds > 300 {
			return fmt.Errorf("node_federation.heartbeat_seconds must be between 2 and 300")
		}
		if cfg.Federation.StaleSeconds < cfg.Federation.HeartbeatSeconds*2 || cfg.Federation.StaleSeconds > 1800 {
			return fmt.Errorf("node_federation.stale_seconds must be at least twice heartbeat_seconds and <= 1800")
		}
	}
	return nil
}

func APIOrigin(cfg Config) string {
	if v := strings.TrimSpace(cfg.Server.PublicOrigin); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "http://" + cfg.Server.Listen
}

func PreviewOrigin(cfg Config) string {
	if v := strings.TrimSpace(cfg.Server.PreviewPublicOrigin); v != "" {
		return strings.TrimRight(v, "/")
	}
	host, port, err := net.SplitHostPort(cfg.Server.PreviewListen)
	if err == nil && isLoopbackHost(host) {
		// Endpoint previews use per-endpoint subdomains for browser-origin
		// isolation. *.localhost is the local special-use namespace, whereas an
		// IP literal cannot have subdomains.
		return "http://" + net.JoinHostPort("localhost", port)
	}
	return "http://" + cfg.Server.PreviewListen
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func requireHTTPSOrigin(origin, message string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(origin))
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return nil, fmt.Errorf("%s", message)
	}
	return u, nil
}

func normalizeOrigin(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return strings.TrimRight(raw, "/")
	}
	return strings.ToLower(u.Scheme + "://" + u.Host)
}

func FederationAdvertiseURL(cfg Config) string {
	if v := strings.TrimSpace(cfg.Federation.AdvertiseURL); v != "" {
		return strings.TrimRight(v, "/")
	}
	host, port, err := net.SplitHostPort(cfg.Federation.Listen)
	if err != nil {
		return ""
	}
	if host != "" && host != "0.0.0.0" && host != "::" {
		return "https://" + net.JoinHostPort(host, port)
	}
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip4 := ip.To4(); ip4 != nil && !ip4.IsLoopback() {
				return "https://" + net.JoinHostPort(ip4.String(), port)
			}
		}
	}
	return "https://" + net.JoinHostPort("127.0.0.1", port)
}
