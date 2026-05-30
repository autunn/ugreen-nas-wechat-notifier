package utils

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSplitIpPortSupportsHostnames(t *testing.T) {
	tests := []struct {
		name        string
		address     string
		defaultPort int
		wantHost    string
		wantPort    int
	}{
		{
			name:        "domain with port",
			address:     "nas.example.com:9443",
			defaultPort: 9999,
			wantHost:    "nas.example.com",
			wantPort:    9443,
		},
		{
			name:        "domain with default port",
			address:     "nas.example.com",
			defaultPort: 5666,
			wantHost:    "nas.example.com",
			wantPort:    5666,
		},
		{
			name:        "url style input",
			address:     "https://nas.example.com:443/path",
			defaultPort: 9999,
			wantHost:    "nas.example.com",
			wantPort:    443,
		},
		{
			name:        "ipv6 with port",
			address:     "[2001:db8::1]:5666",
			defaultPort: 9999,
			wantHost:    "2001:db8::1",
			wantPort:    5666,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, port := SplitIpPort(tt.address, tt.defaultPort)
			if host != tt.wantHost || port != tt.wantPort {
				t.Fatalf("SplitIpPort(%q) = %q, %d; want %q, %d", tt.address, host, port, tt.wantHost, tt.wantPort)
			}
		})
	}
}

func TestDeviceLogFileUsesDeviceIdentity(t *testing.T) {
	got := DeviceLogFile("ugreen", "device-01", "maomao.autunn.top", 443)
	if got == "" {
		t.Fatal("expected log file path")
	}
	if got == DeviceLogFile("ugreen", "device-02", "maomao.autunn.top", 443) {
		t.Fatal("expected different devices to produce different log files")
	}
}

func TestDeviceLogFileUsesUGAppLogDirWhenAvailable(t *testing.T) {
	t.Setenv("UGAPP_LOG_DIR", filepath.Join("runtime", "log"))
	got := DeviceLogFile("ugreen", "device-01", "nas.example.com", 9999)
	if !strings.Contains(got, filepath.Join("runtime", "log")) {
		t.Fatalf("expected log path to use UGAPP_LOG_DIR, got %q", got)
	}
}
