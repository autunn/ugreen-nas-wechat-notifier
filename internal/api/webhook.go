package api

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"nasnotify-go/internal/config"
	"nasnotify-go/internal/nas"
	"nasnotify-go/internal/notify"
	"nasnotify-go/internal/utils"
)

// HandleVerify 处理企业微信的 URL 验证及普通 Webhook 的 GET 请求
func HandleVerify(c *gin.Context) {
	echostr := c.Query("echostr")
	if echostr == "" && (c.Query("text") != "" || c.Query("message") != "" || c.Query("task") != "") {
		HandleMessage(c)
		return
	}

	config.CfgMu.RLock()
	token := config.Config.Token
	aesKeyStr := config.Config.EncodingAESKey
	config.CfgMu.RUnlock()

	msgSig := c.Query("msg_signature")
	timestamp := c.Query("timestamp")
	nonce := c.Query("nonce")

	params := []string{token, timestamp, nonce, echostr}
	sort.Strings(params)
	h := sha1.New()
	h.Write([]byte(strings.Join(params, "")))
	if fmt.Sprintf("%x", h.Sum(nil)) != msgSig {
		c.AbortWithStatus(403)
		return
	}

	aesKey, err := base64.StdEncoding.DecodeString(aesKeyStr + "=")
	if err != nil || len(aesKey) != 32 {
		c.AbortWithStatus(403)
		return
	}

	cipherText, err := base64.StdEncoding.DecodeString(echostr)
	if err != nil || len(cipherText) < 16 {
		c.AbortWithStatus(403)
		return
	}

	block, err := aes.NewCipher(aesKey)
	if err != nil {
		c.AbortWithStatus(403)
		return
	}

	mode := cipher.NewCBCDecrypter(block, aesKey[:16])
	mode.CryptBlocks(cipherText, cipherText)

	msgLen := binary.BigEndian.Uint32(cipherText[16:20])
	c.String(200, string(cipherText[20:20+msgLen]))
}

// HandleMessage 统一处理接收到的通用 Webhook 推送与企业微信交互事件
func HandleMessage(c *gin.Context) {
	bodyBytes, _ := io.ReadAll(c.Request.Body)

	var xmlMsg notify.WeChatXMLMsg
	if len(bodyBytes) > 0 {
		if err := xml.Unmarshal(bodyBytes, &xmlMsg); err == nil && xmlMsg.Encrypt != "" {
			processWechatEvent(c, xmlMsg.Encrypt)
			return
		}
	}

	data := make(map[string]interface{})
	for k, v := range c.Request.URL.Query() {
		if len(v) > 0 {
			data[k] = v[0]
		}
	}

	if len(bodyBytes) > 0 {
		var jsonData map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &jsonData); err == nil {
			for k, v := range jsonData {
				data[k] = v
			}
		} else {
			if len(data) == 0 {
				data["raw_message"] = string(bodyBytes)
			}
		}
	}

	if len(data) > 0 {
		var description strings.Builder
		description.WriteString(fmt.Sprintf("外部 Webhook 触发\n触发时间: %s", time.Now().Format("2006-01-02 15:04:05")))
		for k, v := range data {
			description.WriteString(fmt.Sprintf("\n%s: %v", k, v))
		}
		go notify.WechatPush(description.String())
	}

	c.JSON(200, gin.H{"status": "ok"})
}

// processWechatEvent 解密企业微信的指令并执行对应操作
func processWechatEvent(c *gin.Context, encryptStr string) {
	config.CfgMu.RLock()
	aesKeyStr := config.Config.EncodingAESKey
	config.CfgMu.RUnlock()

	aesKey, err := base64.StdEncoding.DecodeString(aesKeyStr + "=")
	if err != nil || len(aesKey) != 32 {
		c.String(200, "success")
		return
	}

	cipherText, err := base64.StdEncoding.DecodeString(encryptStr)
	if err != nil || len(cipherText) < 16 {
		c.String(200, "success")
		return
	}

	block, err := aes.NewCipher(aesKey)
	if err != nil {
		c.String(200, "success")
		return
	}

	mode := cipher.NewCBCDecrypter(block, aesKey[:16])
	mode.CryptBlocks(cipherText, cipherText)

	msgLen := binary.BigEndian.Uint32(cipherText[16:20])
	plainXmlBytes := cipherText[20 : 20+msgLen]

	var plainMsg notify.WeChatPlainMsg
	if err := xml.Unmarshal(plainXmlBytes, &plainMsg); err == nil {

		// ==================== 1. 拦截菜单点击事件 ====================
		if plainMsg.MsgType == "event" && plainMsg.Event == "click" {
			switch plainMsg.EventKey {
			case "GET_UGREEN_INFO":
				go nas.PushUGreenSystemStatus()
			case "GET_UGREEN_STORAGE":
				go nas.PushUGreenStorageStatus()
			case "GET_UGREEN_UPS":
				go nas.PushUGreenUpsStatus()
			case "GET_UGREEN_DOCKER":
				go nas.PushUGreenDockerStatus()
			case "GET_UGREEN_PS":
				go nas.PushUGreenPsStatus()
			case "GET_UGREEN_BACKUP":
				go nas.PushUGreenBackupStatus()
			case "GET_UGREEN_POWER":
				go nas.PushUGreenPowerStatus()
			case "GET_UGREEN_NOTIFY":
				go nas.PushUGreenNotifyStatus()
			case "GET_UGREEN_PERF":
				go notify.WechatPush("🛠️ **性能控制向导**\n\n请直接在聊天框回复以下指令：\n\n🌀 **风扇控制**\n「风扇 1」: 静音模式\n「风扇 2」: 正常模式\n「风扇 3」: 全速模式\n\n⚡ **CPU 模式**\n「CPU 0」: 高性能\n「CPU 1」: 均衡\n「CPU 2」: 节能")

			case "GET_NAS_WOL":
				go notify.WechatPush("🟢 **远程唤醒向导**\n\n为了精准唤醒目标设备，请直接在聊天框发送指令：\n\n「唤醒 设备名称」\n(例如: 唤醒 绿联)\n\n⚠️ 提示：请确保已在网页后台配置了目标设备的 MAC 地址。")
			}
		}

		// ==================== 2. 拦截用户文本输入 ====================
		if plainMsg.MsgType == "text" {
			content := strings.TrimSpace(plainMsg.Content)
			upperContent := strings.ToUpper(content)

			if strings.HasPrefix(content, "风扇") || strings.HasPrefix(upperContent, "CPU") {
				go nas.HandleUGreenPerfCommand(content)
			} else if strings.HasPrefix(content, "唤醒") {
				targetName := strings.TrimSpace(strings.TrimPrefix(content, "唤醒"))
				go handleWakeCommand(targetName)
			}
		}
	}

	c.String(200, "success")
}

// handleWakeCommand 模糊匹配配置中的设备并下发定向唤醒魔术包
func handleWakeCommand(targetName string) {
	if targetName == "" {
		notify.WechatPush("⚠️ 指令错误：请指定要唤醒的设备名称，例如「唤醒 绿联」")
		return
	}

	config.CfgMu.RLock()
	defer config.CfgMu.RUnlock()

	// 记录同时包含 MAC 地址和配置 IP 的结构
	type wakeTarget struct {
		Mac string
		Ip  string
	}
	var targets []wakeTarget

	for _, cfg := range config.Config.UGreen {
		if strings.Contains(cfg.NotifyTypeName, targetName) && cfg.MacAddress != "" {
			targets = append(targets, wakeTarget{Mac: cfg.MacAddress, Ip: cfg.IpPort})
		}
	}
	for _, cfg := range config.Config.ZSpace {
		if strings.Contains(cfg.NotifyTypeName, targetName) && cfg.MacAddress != "" {
			targets = append(targets, wakeTarget{Mac: cfg.MacAddress, Ip: cfg.IpPort})
		}
	}
	for _, cfg := range config.Config.FnOs {
		if strings.Contains(cfg.NotifyTypeName, targetName) && cfg.MacAddress != "" {
			// 飞牛的字段名为 Server
			targets = append(targets, wakeTarget{Mac: cfg.MacAddress, Ip: cfg.Server})
		}
	}

	if len(targets) == 0 {
		notify.WechatPush(fmt.Sprintf("⚠️ 唤醒失败：未找到包含「%s」的设备，或该设备在后台未配置 MAC 地址。", targetName))
		return
	}

	for _, t := range targets {
		err := utils.WakeOnLAN(t.Mac, t.Ip)
		if err != nil {
			notify.WechatPush(fmt.Sprintf("❌ 向设备(MAC: %s) 发送唤醒包失败: %v", t.Mac, err))
		} else {
			notify.WechatPush(fmt.Sprintf("✅ 唤醒指令已发出，正在通过定向广播唤醒设备 (MAC: %s)", t.Mac))
		}
	}
}
