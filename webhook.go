package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
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

// handleMessage 处理接收到的通用 Webhook 推送 (如 qBittorrent, 群晖 等)
func handleMessage(c *gin.Context) {
	data := make(map[string]interface{})

	// 1. 尝试解析 URL 参数
	for k, v := range c.Request.URL.Query() {
		if len(v) > 0 {
			data[k] = v[0]
		}
	}

	// 2. 尝试解析 Body 中的 JSON
	bodyBytes, _ := io.ReadAll(c.Request.Body)
	if len(bodyBytes) > 0 {
		var jsonData map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &jsonData); err == nil {
			for k, v := range jsonData {
				data[k] = v
			}
		} else {
			// 如果不是 JSON，尝试直接把 raw body 塞进去
			if len(data) == 0 {
				data["raw_message"] = string(bodyBytes)
			}
		}
	}

	// 3. 将组装好的数据交给我们第一步写好的 WechatPush 进行图文推送
	if len(data) > 0 {
		var description strings.Builder
		description.WriteString(fmt.Sprintf("外部 Webhook 触发\n触发时间: %s", time.Now().Format("2006-01-02 15:04:05")))
		for k, v := range data {
			description.WriteString(fmt.Sprintf("\n%s: %v", k, v))
		}
		// 异步推送，避免阻塞调用方的 Webhook 响应
		go WechatPush(description.String())
	}

	c.JSON(200, gin.H{"status": "ok"})
}
