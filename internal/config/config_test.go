package config

import "testing"

func TestDefaultFederationIsOffAndLoopback(t *testing.T) {
	cfg := Default()
	if cfg.Federation.Enabled || cfg.Federation.DiscoveryEnabled {
		t.Fatalf("federation defaults are on: %+v", cfg.Federation)
	}
	if cfg.Federation.Listen != "127.0.0.1:18443" {
		t.Fatalf("federation listen = %s", cfg.Federation.Listen)
	}
	if err := Validate(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestValidateExposure(t *testing.T) {
	c := Default()
	if err := Validate(c); err != nil {
		t.Fatal(err)
	}
	c.Server.Listen = "0.0.0.0:8080"
	if err := Validate(c); err == nil {
		t.Fatal("public plaintext bind accepted")
	}
	c.Server.PublicOrigin = "https://harness.example"
	if err := Validate(c); err == nil {
		t.Fatal("remote control plane accepted without distinct preview origin")
	}
	c.Server.PreviewPublicOrigin = "https://preview.harness.example"
	if err := Validate(c); err != nil {
		t.Fatalf("reverse-proxy origin rejected: %v", err)
	}
	c.Server.PreviewPublicOrigin = c.Server.PublicOrigin
	if err := Validate(c); err == nil {
		t.Fatal("same-origin preview accepted")
	}
}
