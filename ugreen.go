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
	"strconv"
	"strings"
	"time"
)

// ==================== 基础结构体定义 ====================

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

// UGreenSystemInfo 汇总所有监控数据
type UGreenSystemInfo struct {
	UsageCpu    float64 `json:"usageCpu"`
	CpuTemp     float64 `json:"cpuTemp"`
	CpuFan      int     `json:"cpuFan"`
	DeviceFan   int     `json:"deviceFan"`
	UsageMemory float64 `json:"usageMemory"`
	MemoryUsed  int64   `json:"memoryUsed"`
	MemoryTotal int64   `json:"memoryTotal"`

	NetworkReceive       string  `json:"networkReceive"`
	NetworkTransmit      string  `json:"networkTransmit"`
	NetworkReceiveValue  float64 `json:"networkReceiveValue"`
	NetworkReceiveUnit   string  `json:"networkReceiveUnit"`
	NetworkTransmitValue float64 `json:"networkTransmitValue"`
	NetworkTransmitUnit  string  `json:"networkTransmitUnit"`

	System  UGreenSystemStatus  `json:"system"`
	Storage []UGreenStorageItem `json:"storage"`
}

type UGreenSystemStatus struct {
	DevName       string              `json:"dev_name"`
	SystemVersion string              `json:"system_version"`
	Message       string              `json:"message"`
	TotalRunTime  int                 `json:"total_run_time"`
	ServerStatus  int                 `json:"server_status"`
	Status        int                 `json:"status"`
	LastBootDate  string              `json:"last_boot_date"`
	LastBootTime  int64               `json:"last_boot_time"`
	NetworkInfo   []UGreenNetworkInfo `json:"network_info"`
}

type UGreenNetworkInfo struct {
	IPv4  string `json:"ipv4"`
	IPv6  string `json:"ipv6"`
	Label string `json:"label"`
}

type UGreenStorageItem struct {
	Name        string `json:"name"`
	PoolName    string `json:"pool_name"`
	Size        int64  `json:"size"`
	Used        int64  `json:"used"`
	Status      int    `json:"status"`
	Description string `json:"description"`
	StorageName string `json:"storage_name"`
	NotifyPct   int    `json:"capacity_notify_percentage"`
}

// ==================== 核心业务逻辑 ====================

// ProcessUGreen 绿联常规通知增量同步任务
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

		if authInfo == nil {
			newAuth, err := loginUGreen(config.Username, config.Password, ip, port, config.UseSSL)
			if err != nil {
				log.Printf("[绿联] 登录失败: %v\n", err)
				continue
			}
			authInfo = newAuth
		}

		notices, code, err := fetchUGreenNotices(authInfo.TokenID, authInfo.Token, ip, port, config.UseSSL)

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

		lastTime := getLastUGreenTime(logFile)
		var newNotices []UGreenNotice

		for _, notice := range notices {
			if notice.Time > lastTime {
				newNotices = append(newNotices, notice)
			}
		}

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

// PushUGreenSystemStatus 主动获取绿联系统状态并向微信推送报告
func PushUGreenSystemStatus() {
	if len(Config.UGreen) == 0 {
		return
	}

	for _, config := range Config.UGreen {
		ip, port := SplitIpPort(config.IpPort, 9999)
		authInfo := loadUGreenAuthInfo(ip, port)

		if authInfo == nil {
			newAuth, err := loginUGreen(config.Username, config.Password, ip, port, config.UseSSL)
			if err != nil {
				log.Printf("[绿联] 主动推送状态失败 (登录失败): %v\n", err)
				continue
			}
			authInfo = newAuth
		}

		info, err := fetchUGreenSystemInfo(authInfo.TokenID, authInfo.Token, ip, port, config.UseSSL)
		if err != nil {
			// Token 可能过期，尝试重新登录一次
			authInfo, err = loginUGreen(config.Username, config.Password, ip, port, config.UseSSL)
			if err == nil {
				info, err = fetchUGreenSystemInfo(authInfo.TokenID, authInfo.Token, ip, port, config.UseSSL)
			}
		}

		if err != nil {
			log.Printf("[绿联] 获取系统监控数据失败: %v\n", err)
			continue
		}

		pushContent := buildUGreenSystemStatusPushContent(info, config.NotifyTypeName)
		if pushContent != "" {
			WechatPush(pushContent)
			log.Printf("[绿联] 成功为主机 %s 推送系统状态报告\n", config.NotifyTypeName)
		}
	}
}

// ==================== 内部辅助请求与解析逻辑 ====================

func loginUGreen(username, password, ip string, port int, useSSL bool) (*UGreenAuthInfo, error) {
	protocol := "http"
	if useSSL {
		protocol = "https"
	}

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}

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

	pemBytes, err := base64.StdEncoding.DecodeString(xRsaTokenBase64)
	if err != nil {
		return nil, fmt.Errorf("解析 RSA Token 失败: %v", err)
	}

	encPassword, err := RsaEncrypt(string(pemBytes), password)
	if err != nil {
		return nil, err
	}

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

func fetchUGreenSystemInfo(tokenID, token, ip string, port int, useSSL bool) (*UGreenSystemInfo, error) {
	protocol := "http"
	if useSSL {
		protocol = "https"
	}
	baseURL := fmt.Sprintf("%s://%s:%d", protocol, ip, port)

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	info := &UGreenSystemInfo{}

	doGet := func(apiPath string) ([]byte, error) {
		req, _ := http.NewRequest("GET", baseURL+apiPath, nil)
		req.Header.Set("x-specify-language", "zh-CN")
		req.Header.Set("x-ugreen-security-key", tokenID)
		req.Header.Set("x-ugreen-token", token)

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		return io.ReadAll(resp.Body)
	}

	widgetsRaw, err := doGet("/ugreen/v1/desktop/components")
	if err != nil {
		widgetsRaw, err = doGet("/ugreen/v2/desktop/components")
		if err != nil {
			return nil, fmt.Errorf("获取组件列表失败: %v", err)
		}
	}

	var widgets []struct {
		ID   string  `json:"id"`
		Type float64 `json:"type"`
	}
	if json.Unmarshal(widgetsRaw, &widgets) != nil || len(widgets) == 0 {
		var wrapped struct {
			Result []struct {
				ID   string  `json:"id"`
				Type float64 `json:"type"`
			} `json:"result"`
		}
		if json.Unmarshal(widgetsRaw, &wrapped) == nil {
			widgets = wrapped.Result
		}
	}

	for _, w := range widgets {
		dataRaw, err := doGet(fmt.Sprintf("/ugreen/v1/desktop/components/data?id=%s", w.ID))
		if err != nil {
			continue
		}

		var wrapper map[string]json.RawMessage
		if json.Unmarshal(dataRaw, &wrapper) == nil {
			if data, ok := wrapper["data"]; ok && len(data) > 0 {
				dataRaw = data
			} else if result, ok := wrapper["result"]; ok && len(result) > 0 {
				dataRaw = result
			}
		}

		var raw map[string]interface{}
		if json.Unmarshal(dataRaw, &raw) != nil {
			continue
		}

		wType, _ := raw["type"].(float64)
		if int(wType) == 2 {
			json.Unmarshal(dataRaw, &info.System)
		} else if int(wType) == 4 {
			var list struct {
				StorageList []UGreenStorageItem `json:"storage_list"`
			}
			if json.Unmarshal(dataRaw, &list) == nil {
				info.Storage = list.StorageList
			}
		}
	}

	statRaw, err := doGet("/ugreen/v1/taskmgr/stat/get_all")
	if err == nil {
		parseUGreenTaskmgrStats(statRaw, info)
	}

	return info, nil
}

func parseUGreenTaskmgrStats(raw []byte, info *UGreenSystemInfo) {
	var respWrapper struct {
		Data struct {
			Overview struct {
				CPU []struct {
					UsedPercent float64 `json:"used_percent"`
					Temp        float64 `json:"temp"`
				} `json:"cpu"`
				Mem []struct {
					UsedPercent float64 `json:"used_percent"`
				} `json:"mem"`
				Net []struct {
					RecvRate float64 `json:"recv_rate"`
					SendRate float64 `json:"send_rate"`
				} `json:"net"`
				CpuFan []struct {
					Speed int `json:"speed"`
				} `json:"cpu_fan"`
				DeviceFan []struct {
					Speed int `json:"speed"`
				} `json:"device_fan"`
			} `json:"overview"`
			CPU struct {
				Series []struct {
					UsedPercent float64 `json:"used_percent"`
					Temp        float64 `json:"temp"`
				} `json:"series"`
			} `json:"cpu"`
			Mem struct {
				Series []struct {
					UsedPercent float64 `json:"used_percent"`
				} `json:"series"`
				Structure struct {
					Used  int64 `json:"used"`
					Total int64 `json:"total"`
				} `json:"structure"`
			} `json:"mem"`
			Net struct {
				Series []struct {
					RecvRate float64 `json:"recv_rate"`
					SendRate float64 `json:"send_rate"`
				} `json:"series"`
			} `json:"net"`
		} `json:"data"`
	}

	if err := json.Unmarshal(raw, &respWrapper); err != nil {
		return
	}

	data := respWrapper.Data

	if len(data.Overview.CPU) > 0 {
		info.UsageCpu = data.Overview.CPU[0].UsedPercent
		info.CpuTemp = data.Overview.CPU[0].Temp
	} else if len(data.CPU.Series) > 0 {
		info.UsageCpu = data.CPU.Series[0].UsedPercent
		info.CpuTemp = data.CPU.Series[0].Temp
	}

	if len(data.Overview.CpuFan) > 0 {
		info.CpuFan = data.Overview.CpuFan[0].Speed
	}
	if len(data.Overview.DeviceFan) > 0 {
		info.DeviceFan = data.Overview.DeviceFan[0].Speed
	}

	if len(data.Overview.Mem) > 0 {
		info.UsageMemory = data.Overview.Mem[0].UsedPercent
	} else if len(data.Mem.Series) > 0 {
		info.UsageMemory = data.Mem.Series[0].UsedPercent
	}

	info.MemoryUsed = data.Mem.Structure.Used
	info.MemoryTotal = data.Mem.Structure.Total

	var recvRate, sendRate float64
	if len(data.Overview.Net) > 0 {
		recvRate = data.Overview.Net[0].RecvRate
		sendRate = data.Overview.Net[0].SendRate
	} else if len(data.Net.Series) > 0 {
		recvRate = data.Net.Series[0].RecvRate
		sendRate = data.Net.Series[0].SendRate
	}

	info.NetworkReceiveValue = recvRate
	info.NetworkTransmitValue = sendRate
	info.NetworkReceive, info.NetworkReceiveUnit = formatUGreenSpeed(recvRate)
	info.NetworkTransmit, info.NetworkTransmitUnit = formatUGreenSpeed(sendRate)
}

func formatUGreenSpeed(bytesPerSec float64) (string, string) {
	const KB = 1024
	const MB = KB * 1024
	if bytesPerSec >= MB {
		return fmt.Sprintf("%.1fMB/s", bytesPerSec/float64(MB)), "MB/s"
	}
	return fmt.Sprintf("%.1fKB/s", bytesPerSec/float64(KB)), "KB/s"
}

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

func buildUGreenSystemStatusPushContent(info *UGreenSystemInfo, typeName string) string {
	if info == nil {
		return ""
	}
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("📊 %s 系统状态报告\n", typeName))
	builder.WriteString(strings.Repeat("-", 22) + "\n")
	builder.WriteString(fmt.Sprintf("💻 CPU 使用率: %.1f%%\n", info.UsageCpu))
	builder.WriteString(fmt.Sprintf("🌡️ CPU 温度: %.1f°C\n", info.CpuTemp))
	if info.CpuFan > 0 {
		builder.WriteString(fmt.Sprintf("🌀 CPU 风扇: %d RPM\n", info.CpuFan))
	}
	if info.DeviceFan > 0 {
		builder.WriteString(fmt.Sprintf("📦 机箱风扇: %d RPM\n", info.DeviceFan))
	}

	memUsedGB := float64(info.MemoryUsed) / 1024 / 1024 / 1024
	memTotalGB := float64(info.MemoryTotal) / 1024 / 1024 / 1024
	builder.WriteString(fmt.Sprintf("🧠 内存使用: %.1f%% (%.1fG / %.1fG)\n", info.UsageMemory, memUsedGB, memTotalGB))
	builder.WriteString(fmt.Sprintf("🚀 网络下载: %s\n", info.NetworkReceive))
	builder.WriteString(fmt.Sprintf("📤 网络上传: %s\n", info.NetworkTransmit))

	if len(info.Storage) > 0 {
		builder.WriteString(strings.Repeat("-", 22) + "\n")
		builder.WriteString("💾 存储卷状态:\n")
		for _, storage := range info.Storage {
			var usedStr, totalStr string
			if storage.Size > 1024*1024*1024*1024 {
				usedStr = fmt.Sprintf("%.2f TB", float64(storage.Used)/1024/1024/1024/1024)
				totalStr = fmt.Sprintf("%.2f TB", float64(storage.Size)/1024/1024/1024/1024)
			} else {
				usedStr = fmt.Sprintf("%.2f GB", float64(storage.Used)/1024/1024/1024)
				totalStr = fmt.Sprintf("%.2f GB", float64(storage.Size)/1024/1024/1024)
			}
			builder.WriteString(fmt.Sprintf("• %s: %s / %s\n", storage.Name, usedStr, totalStr))
		}
	}
	return builder.String()
}

// ==================== 新增：性能指令发送模块 ====================

// HandleUGreenPerfCommand 解析并执行微信发来的性能控制文本指令
func HandleUGreenPerfCommand(command string) {
	if len(Config.UGreen) == 0 {
		WechatPush("⚠️ 未配置绿联 NAS，无法执行指令")
		return
	}

	// 默认控制配置中的第一台绿联设备
	config := Config.UGreen[0]
	ip, port := SplitIpPort(config.IpPort, 9999)
	authInfo := loadUGreenAuthInfo(ip, port)

	if authInfo == nil {
		newAuth, err := loginUGreen(config.Username, config.Password, ip, port, config.UseSSL)
		if err != nil {
			WechatPush(fmt.Sprintf("❌ 鉴权失败，无法执行指令: %v", err))
			return
		}
		authInfo = newAuth
	}

	upperCommand := strings.ToUpper(strings.TrimSpace(command))
	isFan := strings.HasPrefix(upperCommand, "风扇")
	isCPU := strings.HasPrefix(upperCommand, "CPU")

	var apiPath string
	var payload map[string]interface{}
	var successMsg string

	// 解析参数与构造对应API请求
	if isFan {
		modeStr := strings.TrimSpace(strings.TrimPrefix(upperCommand, "风扇"))
		mode, err := strconv.Atoi(modeStr)
		if err != nil || mode < 1 || mode > 3 {
			WechatPush("⚠️ 风扇指令格式错误。\n支持的模式:\n1: 静音\n2: 正常\n3: 全速\n\n例如发送: 风扇 2")
			return
		}
		apiPath = "/ugreen/v1/hw/fan/mode"
		payload = map[string]interface{}{"mode": mode}
		modes := map[int]string{1: "静音", 2: "正常", 3: "全速"}
		successMsg = fmt.Sprintf("🌀 风扇模式已下发切换指令: %s", modes[mode])
	} else if isCPU {
		modeStr := strings.TrimSpace(strings.TrimPrefix(upperCommand, "CPU"))
		mode, err := strconv.Atoi(modeStr)
		if err != nil || mode < 0 || mode > 2 {
			WechatPush("⚠️ CPU指令格式错误。\n支持的模式:\n0: 高性能\n1: 均衡\n2: 节能\n\n例如发送: CPU 1")
			return
		}
		apiPath = "/ugreen/v1/hw/cpu/mode"
		payload = map[string]interface{}{"mode": mode}
		modes := map[int]string{0: "高性能", 1: "均衡", 2: "节能"}
		successMsg = fmt.Sprintf("⚡ CPU 模式已下发切换指令: %s", modes[mode])
	}

	err := postUGreenAPI(authInfo.TokenID, authInfo.Token, ip, port, config.UseSSL, apiPath, payload)

	if err != nil {
		// Token 失效重试
		authInfo, err = loginUGreen(config.Username, config.Password, ip, port, config.UseSSL)
		if err == nil {
			err = postUGreenAPI(authInfo.TokenID, authInfo.Token, ip, port, config.UseSSL, apiPath, payload)
		}
	}

	if err != nil {
		WechatPush(fmt.Sprintf("❌ 性能指令执行失败:\n%v", err))
	} else {
		WechatPush(successMsg)
	}
}

// postUGreenAPI 通用的绿联 POST 请求封装 (用于参数修改等写操作)
func postUGreenAPI(tokenID, token, ip string, port int, useSSL bool, apiPath string, payload map[string]interface{}) error {
	protocol := "http"
	if useSSL {
		protocol = "https"
	}
	url := fmt.Sprintf("%s://%s:%d%s", protocol, ip, port, apiPath)

	reqBody, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(reqBody))
	req.Header.Set("x-specify-language", "zh-CN")
	req.Header.Set("x-ugreen-security-key", tokenID)
	req.Header.Set("x-ugreen-token", token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("解析接口结果失败")
	}

	// 绿联API成功一般返回 code: 0 或 code: 200
	if code, ok := result["code"].(float64); ok && code != 200 && code != 0 {
		errMsg := "未知错误"
		if msg, ok := result["msg"].(string); ok {
			errMsg = msg
		}
		return fmt.Errorf("返回错误码: %v, 提示: %s", code, errMsg)
	}
	return nil
}
