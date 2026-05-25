package main

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type UGreenAuthInfo struct {
	TokenID string `json:"token_id"`
	Token   string `json:"token"`
}

type UGreenNotice struct {
	Time int64  `json:"time"`
	Body string `json:"body"`
}

type UGreenListResp struct {
	Code int `json:"code"`
	Data struct {
		List []UGreenNotice `json:"List"`
	} `json:"data"`
}

type UGreenLoginResp struct {
	Code int `json:"code"`
	Data struct {
		PublicKey string `json:"public_key"`
		Token     string `json:"token"`
		TokenID   string `json:"token_id"`
	} `json:"data"`
}

// ProcessUGreen 绿联任务主函数
func ProcessUGreen() {
	if len(Config.UGreen) == 0 {
		return
	}

	for _, config := range Config.UGreen {
		ip, port := SplitIpPort(config.IpPort, 9999)
		if !HandleDeviceStatus("绿联", config.NotifyTypeName, ip, port) {
			continue
		}

		logFile := filepath.Join("data", "log", fmt.Sprintf("%s_%d.log", ip, port))
		authInfo := loadUGreenAuthInfo(ip, port)

		// 1. 如果没有缓存的鉴权信息，执行首次登录
		if authInfo == nil {
			newAuth, err := loginUGreen(config.Username, config.Password, ip, port, config.UseSSL)
			if err != nil {
				log.Printf("[绿联] 登录失败: %v\n", err)
				continue
			}
			authInfo = newAuth
		}

		// 2. 尝试获取通知
		notices, code, err := fetchUGreenNotices(authInfo.TokenID, authInfo.Token, ip, port, config.UseSSL)

		// 3. 如果 Code 不是 200 (Token 过期)，重新登录并再次获取
		if err == nil && code != 200 {
			log.Println("[绿联] Token 失效，正在重新鉴权...")
			authInfo, err = loginUGreen(config.Username, config.Password, ip, port, config.UseSSL)
			if err == nil {
				notices, _, err = fetchUGreenNotices(authInfo.TokenID, authInfo.Token, ip, port, config.UseSSL)
			}
		}

		if err != nil {
			log.Printf("[绿联] 获取通知失败: %v\n", err)
			continue
		}

		// 4. 比对本地时间戳并过滤新消息
		lastTime := getLastUGreenTime(logFile)
		var newNotices []UGreenNotice

		for _, notice := range notices {
			if notice.Time > lastTime {
				newNotices = append(newNotices, notice)
			}
		}

		// 5. 处理推送与保存
		fileInfo, err := os.Stat(logFile)
		isFirstRun := os.IsNotExist(err) || fileInfo.Size() == 0

		if isFirstRun {
			saveUGreenNotices(newNotices, logFile)
			pushContent := buildUGreenPushContent(newNotices, config.NotifyTypeName)
			if pushContent != "" {
				WechatPush(pushContent)
				log.Println("[绿联] 新增通知 (首次生成)")
			}
		} else if len(newNotices) > 0 {
			saveUGreenNotices(newNotices, logFile)
			pushContent := buildUGreenPushContent(newNotices, config.NotifyTypeName)
			if pushContent != "" {
				WechatPush(pushContent)
				log.Println("[绿联] 清空文件，新增通知")
			}
		} else {
			log.Printf("[绿联] %s 没有新的通知\n", config.NotifyTypeName)
		}
	}
}

// loginUGreen 执行绿联 RSA 加密登录流程
func loginUGreen(username, password, ip string, port int, useSSL bool) (*UGreenAuthInfo, error) {
	protocol := "http"
	if useSSL {
		protocol = "https"
	}

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}

	// Step 1: 获取加密密码的公钥 (临时 Token)
	checkURL := fmt.Sprintf("%s://%s:%d/ugreen/v1/verify/check?token=", protocol, ip, port)
	checkReqBody, _ := json.Marshal(map[string]string{"username": username})
	req, _ := http.NewRequest("POST", checkURL, bytes.NewBuffer(checkReqBody))
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	xRsaTokenBase64 := resp.Header.Get("X-Rsa-Token")

	// Python 的 jiami 会先进行 base64 decode 得到 PEM 字符串
	pemBytes, err := base64.StdEncoding.DecodeString(xRsaTokenBase64)
	if err != nil {
		return nil, fmt.Errorf("解析 RSA Token 失败: %v", err)
	}

	// 加密密码
	encPassword, err := RsaEncrypt(string(pemBytes), password)
	if err != nil {
		return nil, err
	}

	// Step 2: 正式登录
	loginURL := fmt.Sprintf("%s://%s:%d/ugreen/v1/verify/login", protocol, ip, port)
	loginPayload := map[string]interface{}{
		"username":  username,
		"password":  encPassword,
		"keepalive": true,
		"is_simple": true,
	}
	loginReqBody, _ := json.Marshal(loginPayload)
	req2, _ := http.NewRequest("POST", loginURL, bytes.NewBuffer(loginReqBody))
	req2.Header.Set("x-specify-language", "zh-CN")

	resp2, err := client.Do(req2)
	if err != nil {
		return nil, err
	}
	defer resp2.Body.Close()

	body2, _ := io.ReadAll(resp2.Body)
	var loginResp UGreenLoginResp
	json.Unmarshal(body2, &loginResp)

	if loginResp.Data.Token == "" {
		return nil, fmt.Errorf("获取真正的 Token 失败，返回: %s", string(body2))
	}

	// Step 3: 使用返回的新公钥加密 Token
	pubKeyBytes, _ := base64.StdEncoding.DecodeString(loginResp.Data.PublicKey)
	finalToken, err := RsaEncrypt(string(pubKeyBytes), loginResp.Data.Token)
	if err != nil {
		return nil, err
	}

	authInfo := &UGreenAuthInfo{
		TokenID: loginResp.Data.TokenID,
		Token:   finalToken,
	}
	saveUGreenAuthInfo(ip, port, authInfo)

	return authInfo, nil
}

// fetchUGreenNotices 获取通知列表
func fetchUGreenNotices(tokenID, token, ip string, port int, useSSL bool) ([]UGreenNotice, int, error) {
	protocol := "http"
	if useSSL {
		protocol = "https"
	}
	url := fmt.Sprintf("%s://%s:%d/ugreen/v1/desktop/message/list", protocol, ip, port)

	payload := map[string]interface{}{
		"level": []string{"info", "important", "warning"},
		"page":  1,
		"size":  10,
	}
	reqBody, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(reqBody))
	req.Header.Set("x-specify-language", "zh-CN")
	req.Header.Set("x-ugreen-security-key", tokenID)
	req.Header.Set("x-ugreen-token", token)

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var listResp UGreenListResp
	json.Unmarshal(body, &listResp)

	return listResp.Data.List, listResp.Code, nil
}

// loadUGreenAuthInfo 读取持久化的 Token
func loadUGreenAuthInfo(ip string, port int) *UGreenAuthInfo {
	file := filepath.Join("data", "token", fmt.Sprintf("%s_%d.config", ip, port))
	data, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	var auth UGreenAuthInfo
	json.Unmarshal(data, &auth)
	return &auth
}

func saveUGreenAuthInfo(ip string, port int, auth *UGreenAuthInfo) {
	file := filepath.Join("data", "token", fmt.Sprintf("%s_%d.config", ip, port))
	data, _ := json.Marshal(auth)
	os.WriteFile(file, data, 0644)
}

func getLastUGreenTime(file string) int64 {
	content, err := os.ReadFile(file)
	if err != nil {
		return 0
	}
	lines := strings.Split(string(content), "\n")
	var maxTime int64
	for _, line := range lines {
		parts := strings.SplitN(line, "：", 2)
		if len(parts) == 2 {
			if t, err := time.ParseInLocation("2006-01-02 15:04:05", strings.TrimSpace(parts[0]), time.Local); err == nil {
				if t.Unix() > maxTime {
					maxTime = t.Unix()
				}
			}
		}
	}
	return maxTime
}

func saveUGreenNotices(notices []UGreenNotice, file string) {
	var builder strings.Builder
	for _, notice := range notices {
		// 转换为北京时间格式
		t := time.Unix(notice.Time, 0).In(time.FixedZone("CST", 8*3600))
		builder.WriteString(fmt.Sprintf("%s：%s\n", t.Format("2006-01-02 15:04:05"), notice.Body))
	}
	os.WriteFile(file, []byte(builder.String()), 0644)
}

func buildUGreenPushContent(notices []UGreenNotice, typeName string) string {
	if len(notices) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("%s消息通知（共%d条）", typeName, len(notices)))
	for i, notice := range notices {
		builder.WriteString(fmt.Sprintf("\n\n%d. %s", i+1, notice.Body))
	}
	return builder.String()
}
