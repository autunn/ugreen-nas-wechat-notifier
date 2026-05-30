package utils

import (
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"nasnotify-go/internal/notify"
)

var (
	// deviceOfflineCount 记录设备连续离线的次数
	deviceOfflineCount = make(map[string]int)
	// deviceOfflineMu 保证并发读写的安全
	deviceOfflineMu sync.Mutex
)

// SplitIpPort splits an IP address or hostname from its optional port.
func SplitIpPort(address string, defaultPort int) (string, int) {
	address = strings.TrimSpace(address)
	if address == "" {
		return "", defaultPort
	}

	if strings.Contains(address, "://") {
		if parsedURL, err := url.Parse(address); err == nil && parsedURL.Host != "" {
			address = parsedURL.Host
		}
	}

	if host, portText, err := net.SplitHostPort(address); err == nil {
		if port, parseErr := strconv.Atoi(portText); parseErr == nil {
			return host, port
		}
		return host, defaultPort
	}

	if strings.HasPrefix(address, "[") && strings.HasSuffix(address, "]") {
		return strings.TrimSuffix(strings.TrimPrefix(address, "["), "]"), defaultPort
	}
	if net.ParseIP(address) != nil {
		return address, defaultPort
	}
	if strings.Count(address, ":") == 1 {
		parts := strings.SplitN(address, ":", 2)
		if port, err := strconv.Atoi(parts[1]); err == nil {
			return parts[0], port
		}
		return parts[0], defaultPort
	}

	return address, defaultPort
}

func DeviceLogFile(deviceType, deviceID, host string, port int) string {
	logDir := strings.TrimSpace(os.Getenv("UGAPP_LOG_DIR"))
	if logDir == "" {
		logDir = filepath.Join("data", "log")
	}

	return filepath.Join(
		logDir,
		fmt.Sprintf("%s_%s_%s_%d.log",
			sanitizeLogComponent(deviceType),
			sanitizeLogComponent(deviceID),
			sanitizeLogComponent(host),
			port,
		),
	)
}

func sanitizeLogComponent(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}

	var builder strings.Builder
	for _, r := range value {
		switch {
		case unicode.IsLetter(r), unicode.IsNumber(r):
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}

	return strings.Trim(builder.String(), "_")
}

// CheckPortOpen 检查设备端口是否开放 (超时时间 2 秒)
func CheckPortOpen(ip string, port int) bool {
	address := net.JoinHostPort(ip, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		return false
	}
	if conn != nil {
		conn.Close()
		return true
	}
	return false
}

// HandleDeviceStatus 统一处理设备在线/离线状态与微信推送逻辑
func HandleDeviceStatus(deviceType, deviceName, ip string, port int) bool {
	if deviceName == "" {
		deviceName = "未命名设备"
	}

	// 生成设备的唯一标识 Key
	deviceKey := fmt.Sprintf("%s_%s_%s:%d", deviceType, deviceName, ip, port)
	isOpen := CheckPortOpen(ip, port)

	deviceOfflineMu.Lock()
	defer deviceOfflineMu.Unlock()

	// 1. 如果设备在线
	if isOpen {
		// 检查之前是否有离线记录，如果有，则重置为 0 并发送恢复通知
		if count, exists := deviceOfflineCount[deviceKey]; exists && count > 0 {
			log.Printf("[%s] %s (%s:%d) 已恢复在线", deviceType, deviceName, ip, port)
			deviceOfflineCount[deviceKey] = 0

			recoveryMsg := fmt.Sprintf("✅ 设备恢复在线\n\n设备类型: %s\n设备名称: %s\n地址: %s:%d", deviceType, deviceName, ip, port)
			go notify.WechatPush(recoveryMsg)
		}
		return true
	}

	// 2. 如果设备离线，累加计数器
	deviceOfflineCount[deviceKey]++
	count := deviceOfflineCount[deviceKey]

	// 3. 判断是否需要发送告警
	if count <= 3 {
		log.Printf("[%s] %s (%s:%d) 离线，正在发送第 %d/3 次告警\n", deviceType, deviceName, ip, port, count)
		msg := fmt.Sprintf("⚠️ 设备离线告警\n\n设备类型: %s\n设备名称: %s\n地址: %s:%d\n(连续告警第 %d/3 次)", deviceType, deviceName, ip, port, count)
		go notify.WechatPush(msg)
	} else {
		log.Printf("[%s] %s (%s:%d) 离线，已超过 3 次告警限制，静默处理\n", deviceType, deviceName, ip, port)
	}

	return false
}
