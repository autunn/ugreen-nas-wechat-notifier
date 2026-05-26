package notify

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"nasnotify-go/internal/config"
)

var (
	wechatAccessToken          string
	wechatAccessTokenExpiresAt int64
)

// ==================== 新增：企业微信 XML 数据结构 ====================

// WeChatXMLMsg 接收企业微信推送的加密 XML 数据
type WeChatXMLMsg struct {
	XMLName    xml.Name `xml:"xml"`
	ToUserName string   `xml:"ToUserName"`
	AgentID    string   `xml:"AgentID"`
	Encrypt    string   `xml:"Encrypt"`
}

// WeChatPlainMsg 解密后的明文 XML 结构 (包含事件与普通消息)
type WeChatPlainMsg struct {
	XMLName      xml.Name `xml:"xml"`
	ToUserName   string   `xml:"ToUserName"`
	FromUserName string   `xml:"FromUserName"`
	CreateTime   int64    `xml:"CreateTime"`
	MsgType      string   `xml:"MsgType"`
	Event        string   `xml:"Event"`
	EventKey     string   `xml:"EventKey"`
	Content      string   `xml:"Content"`
}

// ==================== 现有与新增业务逻辑 ====================

// getWeChatToken 获取企业微信 Token (支持代理)
func getWeChatToken(baseURL string) string {
	config.CfgMu.RLock()
	corpID := config.Config.CorpID
	corpSecret := config.Config.CorpSecret
	config.CfgMu.RUnlock()

	if corpID == "" || corpSecret == "" {
		return ""
	}

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
	wechatAccessTokenExpiresAt = time.Now().Unix() + res.Exp - 60
	return wechatAccessToken
}

// WechatPush 发送通知 (图文卡片)
func WechatPush(content string) {
	config.CfgMu.RLock()
	agentIDStr := config.Config.AgentID
	proxyURL := config.Config.ProxyURL
	photoURL := config.Config.PhotoURL
	nasURL := config.Config.NasURL
	config.CfgMu.RUnlock()

	baseURL := "https://qyapi.weixin.qq.com"
	if proxyURL != "" {
		baseURL = strings.TrimRight(proxyURL, "/")
	}

	token := getWeChatToken(baseURL)
	if token == "" {
		log.Println("推送失败：未获取到有效的 AccessToken")
		return
	}

	url := fmt.Sprintf("%s/cgi-bin/message/send?access_token=%s", baseURL, token)

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

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	// 只有在发送失败时才打印日志，避免日常日志过多
	if errcode, ok := result["errcode"].(float64); ok && errcode != 0 {
		log.Printf("企业微信推送失败: %v\n", result)
	}
}

// CreateWechatMenu 自动调用官方 API 创建底部菜单
func CreateWechatMenu() {
	config.CfgMu.RLock()
	agentIDStr := config.Config.AgentID
	proxyURL := config.Config.ProxyURL
	config.CfgMu.RUnlock()

	if agentIDStr == "" {
		return
	}

	baseURL := "https://qyapi.weixin.qq.com"
	if proxyURL != "" {
		baseURL = strings.TrimRight(proxyURL, "/")
	}

	token := getWeChatToken(baseURL)
	if token == "" {
		return
	}

	url := fmt.Sprintf("%s/cgi-bin/menu/create?access_token=%s&agentid=%s", baseURL, token, agentIDStr)

	// 更新为分门别类的复合菜单框架
	payload := map[string]interface{}{
		"button": []map[string]interface{}{
			{
				"name": "📊 监控",
				"sub_button": []map[string]interface{}{
					{"type": "click", "name": "系统概览", "key": "GET_UGREEN_INFO"},
					{"type": "click", "name": "存储状态", "key": "GET_UGREEN_STORAGE"},
					{"type": "click", "name": "UPS电源", "key": "GET_UGREEN_UPS"},
				},
			},
			{
				"name": "🛠️ 服务",
				"sub_button": []map[string]interface{}{
					{"type": "click", "name": "Docker", "key": "GET_UGREEN_DOCKER"},
					{"type": "click", "name": "进程列表", "key": "GET_UGREEN_PS"},
					{"type": "click", "name": "备份任务", "key": "GET_UGREEN_BACKUP"},
				},
			},
			{
				"name": "⚙️ 控制",
				"sub_button": []map[string]interface{}{
					{"type": "click", "name": "电源配置", "key": "GET_UGREEN_POWER"},
					{"type": "click", "name": "性能设置", "key": "GET_UGREEN_PERF"},
					{"type": "click", "name": "系统通知", "key": "GET_UGREEN_NOTIFY"},
				},
			},
		},
	}

	jsonData, _ := json.Marshal(payload)
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("创建微信菜单请求失败: %v\n", err)
		return
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	if errcode, ok := result["errcode"].(float64); ok && errcode == 0 {
		log.Println("✅ 企业微信自定义菜单自动创建成功！")
	} else {
		log.Printf("⚠️ 企业微信菜单创建失败: %v\n", result)
	}
}
