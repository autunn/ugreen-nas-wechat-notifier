package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var (
	wechatAccessToken          string
	wechatAccessTokenExpiresAt int64
)

// getWeChatToken 获取企业微信 Token (支持代理)
func getWeChatToken(baseURL string) string {
	CfgMu.RLock()
	corpID := Config.CorpID
	corpSecret := Config.CorpSecret
	CfgMu.RUnlock()

	if corpID == "" || corpSecret == "" {
		return ""
	}

	// 判断 Token 是否有效
	if wechatAccessToken != "" && wechatAccessTokenExpiresAt > time.Now().Unix() {
		return wechatAccessToken
	}

	url := fmt.Sprintf("%s/cgi-bin/gettoken?corpid=%s&corpsecret=%s", baseURL, corpID, corpSecret)
	resp, err := http.Get(url)
	if err != nil {
		log.Printf("获取Token网络错误: %v\n", err)
		return ""
	}
	defer resp.Body.Close()

	var res struct {
		ErrCode int64  `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		Token   string `json:"access_token"`
		Exp     int64  `json:"expires_in"`
	}
	json.NewDecoder(resp.Body).Decode(&res)

	if res.ErrCode != 0 {
		log.Printf("企业微信API返回错误: %d, %s\n", res.ErrCode, res.ErrMsg)
		return ""
	}

	wechatAccessToken = res.Token
	// 提前一分钟过期，防止边界失效
	wechatAccessTokenExpiresAt = time.Now().Unix() + res.Exp - 60
	return wechatAccessToken
}

// WechatPush 发送通知 (升级为图文卡片)
func WechatPush(content string) {
	CfgMu.RLock()
	agentIDStr := Config.AgentID
	proxyURL := Config.ProxyURL
	photoURL := Config.PhotoURL
	nasURL := Config.NasURL
	CfgMu.RUnlock()

	// 1. 处理代理地址
	baseURL := "https://qyapi.weixin.qq.com"
	if proxyURL != "" {
		baseURL = strings.TrimRight(proxyURL, "/")
	}

	// 2. 获取 Token
	token := getWeChatToken(baseURL)
	if token == "" {
		log.Println("推送失败：未获取到有效的 AccessToken")
		return
	}

	url := fmt.Sprintf("%s/cgi-bin/message/send?access_token=%s", baseURL, token)

	// 3. 处理图片 URL (加入防缓存机制与默认兜底)
	picURL := photoURL
	if picURL == "" {
		picURL = fmt.Sprintf("https://api.vvhan.com/api/wallpaper/acg?rand=%d", time.Now().UnixNano())
	} else {
		connector := "?"
		if strings.Contains(picURL, "?") {
			connector = "&"
		}
		picURL = fmt.Sprintf("%s%sv=%d", picURL, connector, time.Now().UnixNano())
	}

	// 4. 类型转换与 Payload 组装 (图文卡片)
	agentID, _ := strconv.Atoi(agentIDStr)
	payload := map[string]interface{}{
		"touser":  "@all",
		"msgtype": "news",
		"agentid": agentID,
		"news": map[string]interface{}{
			"articles": []map[string]interface{}{
				{
					"title":       "NAS 通知中心",
					"description": content,
					"url":         nasURL,
					"picurl":      picURL,
				},
			},
		},
	}

	jsonData, _ := json.Marshal(payload)
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("发送微信请求失败: %v\n", err)
		return
	}
	defer resp.Body.Close()

	// 打印 API 返回结果，帮助定位问题
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	log.Printf("企业微信推送响应: %v\n", result)
}