package main

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"nasnotify-go/internal/config"
	"nasnotify-go/internal/nas"
	"nasnotify-go/internal/notify"
)

var clawBotCommandMu sync.Mutex
var processedClawBotMessages = make(map[string]time.Time)

func triggerTestPush() error {
	return notify.WechatPush("测试通知\n\n这是一条来自 NasNotify 的测试消息。")
}

func runClawBotCommandLoop() {
	time.Sleep(8 * time.Second)
	for {
		pollClawBotCommandsOnce()
		time.Sleep(20 * time.Second)
	}
}

func pollClawBotCommandsOnce() {
	if !config.IsInitialized() || !notify.ClawBotConfigured() {
		return
	}

	messages, err := notify.GetClawBotMessages()
	if err != nil || len(messages) == 0 {
		return
	}

	for _, msg := range messages {
		text := strings.TrimSpace(msg.Text)
		if text == "" {
			continue
		}
		if notify.MatchClawBotBindingMessage(text) {
			continue
		}
		if !notify.ClawBotBound() || !shouldProcessClawBotCommand(msg) {
			continue
		}
		handleClawBotCommand(text)
	}
}

func shouldProcessClawBotCommand(msg notify.ClawBotMessage) bool {
	clawBotCommandMu.Lock()
	defer clawBotCommandMu.Unlock()

	now := time.Now()
	key := clawBotMessageKey(msg)
	if key == "" {
		return false
	}

	for oldKey, seenAt := range processedClawBotMessages {
		if now.Sub(seenAt) > 10*time.Minute {
			delete(processedClawBotMessages, oldKey)
		}
	}
	if _, exists := processedClawBotMessages[key]; exists {
		return false
	}

	processedClawBotMessages[key] = now
	return true
}

func clawBotMessageKey(msg notify.ClawBotMessage) string {
	if id := strings.TrimSpace(msg.ID); id != "" {
		return "id:" + id
	}
	text := strings.ToLower(strings.TrimSpace(msg.Text))
	if text == "" {
		return ""
	}
	return fmt.Sprintf("text:%s:%s", text, strings.TrimSpace(msg.CreatedAt))
}

func handleClawBotCommand(text string) {
	command := strings.TrimSpace(text)
	normalized := strings.ToLower(command)

	switch {
	case normalized == "菜单" || normalized == "help" || normalized == "menu":
		notify.ClawBotPush(notify.ClawBotMenuText())
	case normalized == "状态" || normalized == "system":
		nas.PushUGreenSystemStatus()
	case normalized == "通知" || normalized == "notice" || normalized == "notify":
		nas.PushUGreenNotifyStatus()
	case normalized == "存储" || normalized == "storage":
		nas.PushUGreenStorageStatus()
	case normalized == "docker":
		nas.PushUGreenDockerStatus()
	case normalized == "进程" || normalized == "ps":
		nas.PushUGreenPsStatus()
	case normalized == "备份" || normalized == "backup":
		nas.PushUGreenBackupStatus()
	case normalized == "电源" || normalized == "power":
		nas.PushUGreenPowerStatus()
	case normalized == "ups":
		nas.PushUGreenUpsStatus()
	case normalized == "测试" || normalized == "test":
		if err := triggerTestPush(); err != nil {
			notify.WechatPush("测试通知发送失败: " + err.Error())
		}
	case strings.HasPrefix(command, "风扇") || strings.HasPrefix(normalized, "fan") || strings.HasPrefix(normalized, "cpu"):
		nas.HandleUGreenPerfCommand(command)
	default:
		notify.ClawBotPush("未识别命令。\n\n" + notify.ClawBotMenuText())
	}
}
