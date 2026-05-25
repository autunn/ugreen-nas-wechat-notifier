package main

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
)

// handleVerify 处理企业微信的 URL 验证及普通 Webhook 的 GET 请求
func handleVerify(c *gin.Context) {
	echostr := c.Query("echostr")
	// 如果没有 echostr，说明可能是外部普通的 GET Webhook 触发
	if echostr == "" && (c.Query("text") != "" || c.Query("message") != "" || c.Query("task") != "") {
		handleMessage(c)
		return
	}

	CfgMu.RLock()
	token := Config.Token
	aesKeyStr := Config.EncodingAESKey
	CfgMu.RUnlock()

	msgSig := c.Query("msg_signature")
	timestamp := c.Query("timestamp")
	nonce := c.Query("nonce")

	// 1. 签名校验
	params := []string{token, timestamp, nonce, echostr}
	sort.Strings(params)
	h := sha1.New()
	h.Write([]byte(strings.Join(params, "")))
	if fmt.Sprintf("%x", h.Sum(nil)) != msgSig {
		c.AbortWithStatus(403)
		return
	}

	// 2. Base64 解码 EncodingAESKey (企业微信的 key 固定 43 位，需补一个 =)
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

	// 3. AES-CBC 解密
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		c.AbortWithStatus(403)
		return
	}

	mode := cipher.NewCBCDecrypter(block, aesKey[:16])
	mode.CryptBlocks(cipherText, cipherText)

	// 4. 去除 16 字节的随机字符串，读取 4 字节的明文长度，并截取真正的 echostr
	msgLen := binary.BigEndian.Uint32(cipherText[16:20])
	c.String(200, string(cipherText[20:20+msgLen]))
}

// handleMessage 统一处理接收到的通用 Webhook 推送与企业微信交互事件
func handleMessage(c *gin.Context) {
	bodyBytes, _ := io.ReadAll(c.Request.Body)

	// 1. 优先尝试按企业微信官方的加密 XML 格式解析
	var xmlMsg WeChatXMLMsg
	if len(bodyBytes) > 0 {
		if err := xml.Unmarshal(bodyBytes, &xmlMsg); err == nil && xmlMsg.Encrypt != "" {
			processWechatEvent(c, xmlMsg.Encrypt)
			return
		}
	}

	// 2. 如果不是 XML，说明是外部系统的普通 Webhook (如 qBittorrent, 群晖 等)
	data := make(map[string]interface{})

	// 尝试解析 URL 参数
	for k, v := range c.Request.URL.Query() {
		if len(v) > 0 {
			data[k] = v[0]
		}
	}

	// 尝试解析 Body 中的 JSON
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

	// 组装数据并交由 WechatPush 发送
	if len(data) > 0 {
		var description strings.Builder
		description.WriteString(fmt.Sprintf("外部 Webhook 触发\n触发时间: %s", time.Now().Format("2006-01-02 15:04:05")))
		for k, v := range data {
			description.WriteString(fmt.Sprintf("\n%s: %v", k, v))
		}
		go WechatPush(description.String())
	}

	c.JSON(200, gin.H{"status": "ok"})
}

// processWechatEvent 解密企业微信的指令并执行对应操作
func processWechatEvent(c *gin.Context, encryptStr string) {
	CfgMu.RLock()
	aesKeyStr := Config.EncodingAESKey
	CfgMu.RUnlock()

	// 解密准备
	aesKey, err := base64.StdEncoding.DecodeString(aesKeyStr + "=")
	if err != nil || len(aesKey) != 32 {
		c.String(200, "success") // 企业微信要求接收方只要不抛错，都应返回 success 或空串
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

	// AES-CBC 解密
	mode := cipher.NewCBCDecrypter(block, aesKey[:16])
	mode.CryptBlocks(cipherText, cipherText)

	// 解析出真正的 XML 内容 (跳过 16 字节随机码，读取 4 字节长度)
	msgLen := binary.BigEndian.Uint32(cipherText[16:20])
	plainXmlBytes := cipherText[20 : 20+msgLen]

	var plainMsg WeChatPlainMsg
	if err := xml.Unmarshal(plainXmlBytes, &plainMsg); err == nil {
		// 拦截菜单点击事件
		if plainMsg.MsgType == "event" && plainMsg.Event == "click" {
			// 匹配我们第一步中创建的按钮 Key
			if plainMsg.EventKey == "GET_UGREEN_STATUS" {
				// 异步触发状态推送
				go PushUGreenSystemStatus()
			}
		}
	}

	// 必须在 5 秒内返回 200 状态码和 success 文本，否则微信会反复重试报错
	c.String(200, "success")
}
