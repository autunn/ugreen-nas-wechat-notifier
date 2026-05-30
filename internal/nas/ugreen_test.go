package nas

import (
	"strings"
	"testing"
)

func TestWechatProgressBar(t *testing.T) {
	tests := []struct {
		name    string
		percent float64
		width   int
		want    string
	}{
		{name: "empty", percent: 0, width: 5, want: "▱▱▱▱▱"},
		{name: "half", percent: 50, width: 6, want: "▰▰▰▱▱▱"},
		{name: "full", percent: 100, width: 4, want: "▰▰▰▰"},
		{name: "clamped", percent: 180, width: 4, want: "▰▰▰▰"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := wechatProgressBar(tt.percent, tt.width); got != tt.want {
				t.Fatalf("wechatProgressBar() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatBytesHuman(t *testing.T) {
	tests := []struct {
		size int64
		want string
	}{
		{size: 0, want: "0 B"},
		{size: 512, want: "512 B"},
		{size: 1024 * 1024 * 5, want: "5.00 MB"},
		{size: 1024 * 1024 * 1024 * 12, want: "12.0 GB"},
	}

	for _, tt := range tests {
		if got := formatBytesHuman(tt.size); got != tt.want {
			t.Fatalf("formatBytesHuman(%d) = %q, want %q", tt.size, got, tt.want)
		}
	}
}

func TestBuildUGreenSystemStatusPushContentUsesCharts(t *testing.T) {
	content := buildUGreenSystemStatusPushContent(&UGreenSystemInfo{
		UsageCpu:        25,
		CpuTemp:         45,
		UsageMemory:     50,
		MemoryUsed:      4 * 1024 * 1024 * 1024,
		MemoryTotal:     8 * 1024 * 1024 * 1024,
		NetworkReceive:  "1.2MB/s",
		NetworkTransmit: "88.0KB/s",
	}, "本机绿联 NAS")

	for _, want := range []string{"系统状态概览", "CPU", "▰▰▰", "MEM", "Network Stream"} {
		if !strings.Contains(content, want) {
			t.Fatalf("content does not contain %q:\n%s", want, content)
		}
	}
}

func TestParseUGreenPerfCommand(t *testing.T) {
	tests := []struct {
		name       string
		command    string
		wantAction string
		wantMode   string
		wantOK     bool
	}{
		{
			name:       "compact fan command",
			command:    "风扇2",
			wantAction: "风扇",
			wantMode:   "2",
			wantOK:     true,
		},
		{
			name:       "spaced fan command",
			command:    "风扇 2",
			wantAction: "风扇",
			wantMode:   "2",
			wantOK:     true,
		},
		{
			name:       "compact cpu command",
			command:    "CPU1",
			wantAction: "CPU",
			wantMode:   "1",
			wantOK:     true,
		},
		{
			name:       "lowercase spaced cpu command",
			command:    "cpu 2",
			wantAction: "CPU",
			wantMode:   "2",
			wantOK:     true,
		},
		{
			name:       "english fan alias",
			command:    "fan3",
			wantAction: "FAN",
			wantMode:   "3",
			wantOK:     true,
		},
		{
			name:    "missing fan mode",
			command: "风扇",
			wantOK:  false,
		},
		{
			name:    "unsupported command",
			command: "abc",
			wantOK:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action, mode, ok := parseUGreenPerfCommand(tt.command)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if action != tt.wantAction || mode != tt.wantMode {
				t.Fatalf("parseUGreenPerfCommand(%q) = (%q, %q), want (%q, %q)",
					tt.command, action, mode, tt.wantAction, tt.wantMode)
			}
		})
	}
}
