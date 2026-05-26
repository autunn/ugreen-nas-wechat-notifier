package utils

import (
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"nasnotify-go/internal/notify"
)

var (
	// deviceOfflineCount 记录设备连续离线的次数
	deviceOfflineCount = make(map[string]int)
	// deviceOfflineMu 保证并发读写的安全
	deviceOfflineMu sync.Mutex
)

// SplitIpPort 拆分 IP 和端口
func SplitIpPort(ipPort string, defaultPort int) (string, int) {
	parts := strings.Split(ipPort, ":")
	ip := parts[0]
	port := defaultPort
	if len(parts) > 1 {
		if p, err := strconv.Atoi(parts[1]); err == nil {
			port = p
		}
	}
	return ip, port
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

// WakeOnLAN 发送 UDP 魔术包唤醒局域网设备 (散弹枪模式：全局广播 + 定向广播 + 单播穿透)
func WakeOnLAN(macAddr string, deviceIpPort string) error {
	if macAddr == "" {
		return errors.New("MAC地址为空")
	}
	macStr := strings.ReplaceAll(macAddr, ":", "")
	macStr = strings.ReplaceAll(macStr, "-", "")
	if len(macStr) != 12 {
		return errors.New("无效的MAC地址格式")
	}

	macBytes, err := hex.DecodeString(macStr)
	if err != nil {
		return err
	}

	var packet []byte
	// 6 字节的 0xFF
	for i := 0; i < 6; i++ {
		packet = append(packet, 0xFF)
	}
	// 16 次 MAC 地址
	for i := 0; i < 16; i++ {
		packet = append(packet, macBytes...)
	}

	ip, _ := SplitIpPort(deviceIpPort, 0)
	parsedIP := net.ParseIP(ip)

	// 目标地址池
	var targets []string
	targets = append(targets, "255.255.255.255:9") // 1. 全局广播 (兜底)

	if parsedIP != nil && parsedIP.To4() != nil {
		ipv4 := parsedIP.To4()
		// 2. 定向广播 (例如 192.168.1.255)
		directedBroadcast := fmt.Sprintf("%d.%d.%d.255:9", ipv4[0], ipv4[1], ipv4[2])
		targets = append(targets, directedBroadcast)

		// 3. 单播穿透 (例如 192.168.1.9) - 专治 Mac Docker 网络隔离
		unicastTarget := fmt.Sprintf("%s:9", ip)
		targets = append(targets, unicastTarget)
	}

	// 循环向所有可能的地址轰炸唤醒包
	var lastErr error
	successCount := 0
	for _, target := range targets {
		conn, err := net.DialTimeout("udp", target, 2*time.Second)
		if err != nil {
			lastErr = err
			continue
		}
		_, err = conn.Write(packet)
		conn.Close()
		if err == nil {
			successCount++
		} else {
			lastErr = err
		}
	}

	if successCount == 0 && lastErr != nil {
		return fmt.Errorf("所有穿透策略均失败: %v", lastErr)
	}

	return nil
}
