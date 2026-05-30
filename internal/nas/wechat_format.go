package nas

import (
	"fmt"
	"math"
	"strings"
	"time"
)

const wechatChartWidth = 12

func wechatCardHeader(icon, title, device string) string {
	device = strings.TrimSpace(device)
	if device == "" {
		device = "绿联 NAS"
	}

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("╭─ %s %s\n", icon, title))
	builder.WriteString(fmt.Sprintf("│ NAS  %s\n", device))
	builder.WriteString(fmt.Sprintf("│ TS   %s\n", time.Now().Format("01-02 15:04")))
	builder.WriteString("╰────────────────\n")
	return builder.String()
}

func wechatSection(title string) string {
	return "\n◆ " + title + "\n"
}

func wechatPercentLine(label string, percent float64) string {
	percent = clampPercent(percent)
	return fmt.Sprintf("%s  %5.1f%%  %s", label, percent, wechatProgressBar(percent, wechatChartWidth))
}

func wechatCountLine(label string, current, total int) string {
	percent := 0.0
	if total > 0 {
		percent = float64(current) / float64(total) * 100
	}
	return fmt.Sprintf("%s  %d/%d  %s", label, current, total, wechatProgressBar(percent, wechatChartWidth))
}

func wechatProgressBar(percent float64, width int) string {
	if width <= 0 {
		width = wechatChartWidth
	}

	percent = clampPercent(percent)
	filled := int(math.Round(percent / 100 * float64(width)))
	if percent > 0 && filled == 0 {
		filled = 1
	}
	if filled > width {
		filled = width
	}

	return strings.Repeat("▰", filled) + strings.Repeat("▱", width-filled)
}

func clampPercent(percent float64) float64 {
	switch {
	case math.IsNaN(percent), math.IsInf(percent, 0), percent < 0:
		return 0
	case percent > 100:
		return 100
	default:
		return percent
	}
}

func formatBytesHuman(size int64) string {
	if size <= 0 {
		return "0 B"
	}

	value := float64(size)
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}

	if unit == 0 {
		return fmt.Sprintf("%d %s", size, units[unit])
	}
	if value >= 10 {
		return fmt.Sprintf("%.1f %s", value, units[unit])
	}
	return fmt.Sprintf("%.2f %s", value, units[unit])
}

func trimDisplayText(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	if maxRunes <= 0 {
		return text
	}

	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	if maxRunes <= 1 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-1]) + "…"
}

func fallbackText(text, fallback string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return fallback
	}
	return text
}

func enabledStatus(enabled bool) string {
	if enabled {
		return "✅ 开启"
	}
	return "⬜ 关闭"
}
