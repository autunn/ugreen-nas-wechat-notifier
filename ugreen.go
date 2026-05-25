package main

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ==================== 基础结构体定义 ====================

type UGreenAuthInfo struct {
	TokenID   string `json:"token_id"`
	Token     string `json:"token"`      // 修正：此处直接保存明文 Token，不做提前加密
	PublicKey string `json:"public_key"` // 保存公钥用于加密
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

// ==================== 核心与菜单触发业务 ====================

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
				continue
			}
			authInfo = newAuth
		}

		notices, code, err := fetchUGreenNotices(authInfo, ip, port, config.UseSSL)
		if err == nil && code != 200 {
			authInfo, err = loginUGreen(config.Username, config.Password, ip, port, config.UseSSL)
			if err == nil {
				notices, _, err = fetchUGreenNotices(authInfo, ip, port, config.UseSSL)
			}
		}

		if err != nil {
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
			}
		} else if len(newNotices) > 0 {
			saveUGreenNotices(newNotices, logFile)
			pushContent := buildUGreenPushContent(newNotices, config.NotifyTypeName)
			if pushContent != "" {
				WechatPush(pushContent)
			}
		}
	}
}

// PushUGreenSystemStatus 菜单触发：系统概览
func PushUGreenSystemStatus() {
	if len(Config.UGreen) == 0 {
		return
	}
	config := Config.UGreen[0]
	ip, port := SplitIpPort(config.IpPort, 9999)
	authInfo := ensureAuth(config.Username, config.Password, ip, port, config.UseSSL)
	if authInfo == nil {
		return
	}

	info, err := fetchUGreenSystemInfo(authInfo, ip, port, config.UseSSL)
	if err != nil {
		authInfo = ensureAuth(config.Username, config.Password, ip, port, config.UseSSL)
		info, _ = fetchUGreenSystemInfo(authInfo, ip, port, config.UseSSL)
	}

	pushContent := buildUGreenSystemStatusPushContent(info, config.NotifyTypeName)
	if pushContent != "" {
		WechatPush(pushContent)
	}
}

// PushUGreenStorageStatus 菜单触发：独立存储状态
func PushUGreenStorageStatus() {
	if len(Config.UGreen) == 0 {
		return
	}
	config := Config.UGreen[0]
	ip, port := SplitIpPort(config.IpPort, 9999)
	authInfo := ensureAuth(config.Username, config.Password, ip, port, config.UseSSL)
	if authInfo == nil {
		return
	}

	info, _ := fetchUGreenSystemInfo(authInfo, ip, port, config.UseSSL)
	if info != nil && len(info.Storage) > 0 {
		var builder strings.Builder
		builder.WriteString(fmt.Sprintf("💾 %s 存储卷状态详情\n", config.NotifyTypeName))
		builder.WriteString(strings.Repeat("-", 22) + "\n")
		for _, storage := range info.Storage {
			var usedStr, totalStr string
			if storage.Size > 1024*1024*1024*1024 {
				usedStr = fmt.Sprintf("%.2f TB", float64(storage.Used)/1024/1024/1024/1024)
				totalStr = fmt.Sprintf("%.2f TB", float64(storage.Size)/1024/1024/1024/1024)
			} else {
				usedStr = fmt.Sprintf("%.2f GB", float64(storage.Used)/1024/1024/1024)
				totalStr = fmt.Sprintf("%.2f GB", float64(storage.Size)/1024/1024/1024)
			}
			usagePct := float64(storage.Used) / float64(storage.Size) * 100
			builder.WriteString(fmt.Sprintf("🔹 %s (%s)\n", storage.Name, storage.PoolName))
			builder.WriteString(fmt.Sprintf("容量: %s / %s (%.1f%%)\n", usedStr, totalStr, usagePct))
			builder.WriteString(fmt.Sprintf("告警阈值: %d%%\n\n", storage.NotifyPct))
		}
		WechatPush(strings.TrimSpace(builder.String()))
	} else {
		WechatPush("⚠️ 未获取到存储卷信息")
	}
}

// PushUGreenNotifyStatus 菜单触发：主动拉取最新通知
func PushUGreenNotifyStatus() {
	if len(Config.UGreen) == 0 {
		return
	}
	config := Config.UGreen[0]
	ip, port := SplitIpPort(config.IpPort, 9999)
	authInfo := ensureAuth(config.Username, config.Password, ip, port, config.UseSSL)
	if authInfo == nil {
		return
	}

	notices, _, _ := fetchUGreenNotices(authInfo, ip, port, config.UseSSL)
	if len(notices) > 0 {
		WechatPush(buildUGreenPushContent(notices, config.NotifyTypeName+" 近期通知"))
	} else {
		WechatPush("当前没有新的系统通知。")
	}
}

// PushUGreenDockerStatus 菜单触发：Docker 状态
func PushUGreenDockerStatus() {
	if len(Config.UGreen) == 0 {
		return
	}
	config := Config.UGreen[0]
	ip, port := SplitIpPort(config.IpPort, 9999)
	authInfo := ensureAuth(config.Username, config.Password, ip, port, config.UseSSL)
	if authInfo == nil {
		return
	}

	ovRaw, err := requestUGreenDeepAPI(authInfo, ip, port, config.UseSSL, "GET", "/ugreen/v1/docker/view/ObtainOverviewInfo", nil, nil)
	if err != nil {
		WechatPush("⚠️ 获取 Docker 状态失败: " + err.Error())
		return
	}
	var overview struct {
		RunContainerCount int `json:"runContainerCount"`
		ContainerCount    int `json:"containerCount"`
		ImageCount        int `json:"imageCount"`
		CpuUsed           int `json:"cpuUsed"`
	}
	json.Unmarshal(ovRaw, &overview)

	listRaw, _ := requestUGreenDeepAPI(authInfo, ip, port, config.UseSSL, "POST", "/ugreen/v1/docker/container/ContainerListV2", nil, map[string]interface{}{"pageNum": 1, "pageSize": 200})
	var list struct {
		Result []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"result"`
	}
	json.Unmarshal(listRaw, &list)

	var builder strings.Builder
	builder.WriteString("🐳 Docker 运行概览\n")
	builder.WriteString(strings.Repeat("-", 22) + "\n")
	builder.WriteString(fmt.Sprintf("运行中容器: %d / %d\n", overview.RunContainerCount, overview.ContainerCount))
	builder.WriteString(fmt.Sprintf("本地镜像数: %d\n", overview.ImageCount))
	builder.WriteString(fmt.Sprintf("整体CPU负载: %d%%\n\n", overview.CpuUsed))

	builder.WriteString("🟢 运行中的容器:\n")
	count := 0
	for _, c := range list.Result {
		if c.Status == "running" || c.Status == "Up" {
			builder.WriteString(fmt.Sprintf("▪️ %s\n", c.Name))
			count++
		}
	}
	if count == 0 {
		builder.WriteString("当前无运行中容器\n")
	}

	WechatPush(strings.TrimSpace(builder.String()))
}

// PushUGreenPsStatus 菜单触发：进程列表
func PushUGreenPsStatus() {
	if len(Config.UGreen) == 0 {
		return
	}
	config := Config.UGreen[0]
	ip, port := SplitIpPort(config.IpPort, 9999)
	authInfo := ensureAuth(config.Username, config.Password, ip, port, config.UseSSL)
	if authInfo == nil {
		return
	}

	raw, err := requestUGreenDeepAPI(authInfo, ip, port, config.UseSSL, "GET", "/ugreen/v1/taskmgr/services/processes", nil, nil)
	if err != nil {
		WechatPush("⚠️ 获取进程列表失败: " + err.Error())
		return
	}

	var resp struct {
		Processes struct {
			List []struct {
				Name    string `json:"name"`
				Consume struct {
					CPU    float64 `json:"cpu_used_percent"`
					Memory float64 `json:"mem_used_percent"`
				} `json:"consume"`
			} `json:"list"`
		} `json:"processes"`
	}
	json.Unmarshal(raw, &resp)

	sort.Slice(resp.Processes.List, func(i, j int) bool {
		return resp.Processes.List[i].Consume.CPU > resp.Processes.List[j].Consume.CPU
	})

	var builder strings.Builder
	builder.WriteString("📈 系统进程占用 (TOP 5)\n")
	builder.WriteString(strings.Repeat("-", 22) + "\n")
	for i, p := range resp.Processes.List {
		if i >= 5 {
			break
		}
		builder.WriteString(fmt.Sprintf("%d. %s\n   CPU: %.1f%% | 内存: %.1f%%\n", i+1, p.Name, p.Consume.CPU, p.Consume.Memory))
	}

	WechatPush(strings.TrimSpace(builder.String()))
}

// PushUGreenBackupStatus 菜单触发：备份任务
func PushUGreenBackupStatus() {
	if len(Config.UGreen) == 0 {
		return
	}
	config := Config.UGreen[0]
	ip, port := SplitIpPort(config.IpPort, 9999)
	authInfo := ensureAuth(config.Username, config.Password, ip, port, config.UseSSL)
	if authInfo == nil {
		return
	}

	raw, err := requestUGreenDeepAPI(authInfo, ip, port, config.UseSSL, "GET", "/ugreen/v2/web/syncbackup/task/list", map[string]string{"backup_type": "backup", "page": "1", "size": "100"}, nil)
	if err != nil {
		WechatPush("⚠️ 获取备份任务失败: " + err.Error())
		return
	}

	var result struct {
		List []struct {
			TaskName     string `json:"task_name"`
			Status       int    `json:"status"`
			LastSyncTime int64  `json:"last_sync_time"`
		} `json:"list"`
	}
	json.Unmarshal(raw, &result)

	var builder strings.Builder
	builder.WriteString("🔄 备份任务状态\n")
	builder.WriteString(strings.Repeat("-", 22) + "\n")
	if len(result.List) == 0 {
		builder.WriteString("当前没有配置备份任务\n")
	} else {
		for _, t := range result.List {
			statusStr := "未知"
			switch t.Status {
			case 0:
				statusStr = "已停止 ⏸️"
			case 1:
				statusStr = "正常 ✅"
			case 2:
				statusStr = "运行中 🔄"
			case 3:
				statusStr = "已暂停 ⏸️"
			case 4:
				statusStr = "错误 ❌"
			}
			lastSync := "从未运行"
			if t.LastSyncTime > 0 {
				lastSync = time.Unix(t.LastSyncTime, 0).Format("2006-01-02 15:04")
			}
			builder.WriteString(fmt.Sprintf("📁 %s\n   状态: %s\n   最后同步: %s\n\n", t.TaskName, statusStr, lastSync))
		}
	}
	WechatPush(strings.TrimSpace(builder.String()))
}

// PushUGreenPowerStatus 菜单触发：电源配置
func PushUGreenPowerStatus() {
	if len(Config.UGreen) == 0 {
		return
	}
	config := Config.UGreen[0]
	ip, port := SplitIpPort(config.IpPort, 9999)
	authInfo := ensureAuth(config.Username, config.Password, ip, port, config.UseSSL)
	if authInfo == nil {
		return
	}

	raw, err := requestUGreenDeepAPI(authInfo, ip, port, config.UseSSL, "GET", "/ugreen/v1/hardware/power/config", nil, nil)
	if err != nil {
		WechatPush("⚠️ 获取电源配置失败: " + err.Error())
		return
	}

	var cfg struct {
		Data struct {
			PowerBoot     bool   `json:"power_boot"`
			WakeOn        bool   `json:"wake_on"`
			HardDriveFlag bool   `json:"hard_drive_flag"`
			HardDriveTime int    `json:"hard_drive_time"`
			HardDriveUnit string `json:"hard_drive_unit"`
		} `json:"data"`
	}
	json.Unmarshal(raw, &cfg)
	if !cfg.Data.PowerBoot && !cfg.Data.HardDriveFlag && !cfg.Data.WakeOn {
		json.Unmarshal(raw, &cfg.Data)
	}

	var builder strings.Builder
	builder.WriteString("⚡ 电源与休眠配置\n")
	builder.WriteString(strings.Repeat("-", 22) + "\n")

	statusMap := func(b bool) string {
		if b {
			return "已开启 ✅"
		}
		return "已关闭 ❌"
	}

	unitMap := func(u string) string {
		if u == "H" {
			return "小时"
		}
		return "分钟"
	}

	builder.WriteString(fmt.Sprintf("来电自启: %s\n", statusMap(cfg.Data.PowerBoot)))
	builder.WriteString(fmt.Sprintf("网络唤醒(WOL): %s\n", statusMap(cfg.Data.WakeOn)))
	builder.WriteString(fmt.Sprintf("内置硬盘休眠: %s\n", statusMap(cfg.Data.HardDriveFlag)))
	if cfg.Data.HardDriveFlag {
		builder.WriteString(fmt.Sprintf("休眠等待时间: %d %s\n", cfg.Data.HardDriveTime, unitMap(cfg.Data.HardDriveUnit)))
	}

	WechatPush(strings.TrimSpace(builder.String()))
}

// ==================== 性能控制与加密 API 请求 ====================

// HandleUGreenPerfCommand 解析并执行微信发来的性能控制文本指令
func HandleUGreenPerfCommand(command string) {
	if len(Config.UGreen) == 0 {
		return
	}
	config := Config.UGreen[0]
	ip, port := SplitIpPort(config.IpPort, 9999)
	authInfo := ensureAuth(config.Username, config.Password, ip, port, config.UseSSL)

	if authInfo == nil || authInfo.PublicKey == "" {
		authInfo, _ = loginUGreen(config.Username, config.Password, ip, port, config.UseSSL)
		if authInfo == nil {
			WechatPush("❌ 控制指令失败：登录凭证无效")
			return
		}
	}

	upperCommand := strings.ToUpper(strings.TrimSpace(command))
	isFan := strings.HasPrefix(upperCommand, "风扇")
	isCPU := strings.HasPrefix(upperCommand, "CPU")

	var successMsg string
	var err error

	if isFan {
		modeStr := strings.TrimSpace(strings.TrimPrefix(upperCommand, "风扇"))
		mode, _ := strconv.Atoi(modeStr)
		if mode < 1 || mode > 3 {
			WechatPush("⚠️ 风扇指令格式错误。\n1: 静音 | 2: 正常 | 3: 全速\n例如: 风扇 2")
			return
		}
		_, err = requestUGreenDeepAPI(authInfo, ip, port, config.UseSSL, "GET", "/ugreen/v1/hardware/fan/start", map[string]string{"mode": strconv.Itoa(mode)}, nil)
		modes := map[int]string{1: "静音", 2: "正常", 3: "全速"}
		successMsg = fmt.Sprintf("🌀 风扇模式已成功切换为: %s", modes[mode])
	} else if isCPU {
		modeStr := strings.TrimSpace(strings.TrimPrefix(upperCommand, "CPU"))
		mode, _ := strconv.Atoi(modeStr)
		if mode < 0 || mode > 2 {
			WechatPush("⚠️ CPU指令格式错误。\n0: 高性能 | 1: 均衡 | 2: 节能\n例如: CPU 1")
			return
		}
		_, err = requestUGreenDeepAPI(authInfo, ip, port, config.UseSSL, "POST", "/ugreen/v1/hardware/cpu/frequency", nil, map[string]interface{}{"frequency": mode})
		modes := map[int]string{0: "高性能", 1: "均衡", 2: "节能"}
		successMsg = fmt.Sprintf("⚡ CPU 模式已成功切换为: %s", modes[mode])
	}

	if err != nil {
		WechatPush(fmt.Sprintf("❌ 指令执行失败: %v", err))
	} else {
		WechatPush(successMsg)
	}
}

// requestUGreenDeepAPI 绿联深层加密 API 请求器 (支持 GET/POST 加密)
func requestUGreenDeepAPI(authInfo *UGreenAuthInfo, ip string, port int, useSSL bool, method string, apiPath string, params map[string]string, body map[string]interface{}) ([]byte, error) {
	protocol := "http"
	if useSSL {
		protocol = "https"
	}

	// 生成 32 位随机 AES 密钥
	aesKey := strings.ReplaceAll(time.Now().Format("20060102150405.000000000")+"abc", ".", "")
	if len(aesKey) > 32 {
		aesKey = aesKey[:32]
	} else {
		aesKey = fmt.Sprintf("%-32s", aesKey)
	}

	urlStr := fmt.Sprintf("%s://%s:%d%s", protocol, ip, port, apiPath)

	// 1. 处理 GET 加密参数
	if len(params) > 0 {
		var query []string
		for k, v := range params {
			query = append(query, fmt.Sprintf("%s=%s", k, v))
		}
		rawQuery := strings.Join(query, "&")
		encQuery, _ := AESGCMEncrypt(aesKey, rawQuery)
		urlStr += "?encrypt_query=" + encQuery
	}

	// 2. 处理 POST 加密请求体
	var bodyReader io.Reader
	if body != nil {
		bodyJSON, _ := json.Marshal(body)
		encBody, _ := AESGCMEncrypt(aesKey, string(bodyJSON))
		encReq := map[string]string{"encrypt_req_body": encBody}
		encReqJSON, _ := json.Marshal(encReq)
		bodyReader = bytes.NewReader(encReqJSON)
	}

	req, _ := http.NewRequest(method, urlStr, bodyReader)

	// 3. 头部权限与密钥装载
	securityCode, _ := RsaEncrypt(authInfo.PublicKey, aesKey)
	ugreenToken, _ := RsaEncrypt(authInfo.PublicKey, authInfo.Token) // 这里加密给请求头

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Client-Id", "cli-go-tool")
	req.Header.Set("UG-Agent", "PC/WEB")
	req.Header.Set("X-Specify-Language", "zh-CN")
	req.Header.Set("X-Ugreen-Security-Code", securityCode)
	req.Header.Set("X-Ugreen-Security-Key", MD5Hex(authInfo.Token)) // MD5 取的是明文 Token
	req.Header.Set("X-Ugreen-Token", ugreenToken)

	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)

	// 4. 解析并解密响应体
	var result map[string]interface{}
	if err := json.Unmarshal(raw, &result); err == nil {
		if code, ok := result["code"].(float64); ok && code != 200 && code != 0 {
			msg := "未知错误"
			if m, ok := result["msg"].(string); ok {
				msg = m
			}
			return nil, fmt.Errorf("返回错误码: %v, %s", code, msg)
		}

		if dataMap, ok := result["data"].(map[string]interface{}); ok {
			if encResp, ok := dataMap["encrypt_resp_body"].(string); ok {
				dec, _ := AESGCMDecrypt(aesKey, encResp)
				return []byte(dec), nil
			}
		}
	}
	return raw, nil
}

// ==================== 内部辅助请求与解析逻辑 ====================

func ensureAuth(username, password, ip string, port int, useSSL bool) *UGreenAuthInfo {
	authInfo := loadUGreenAuthInfo(ip, port)
	if authInfo != nil {
		return authInfo
	}
	newAuth, _ := loginUGreen(username, password, ip, port, useSSL)
	return newAuth
}

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

	pemBytes, err := base64.StdEncoding.DecodeString(resp.Header.Get("X-Rsa-Token"))
	if err != nil {
		return nil, err
	}

	encPassword, err := RsaEncrypt(string(pemBytes), password)
	if err != nil {
		return nil, err
	}

	loginURL := fmt.Sprintf("%s://%s:%d/ugreen/v1/verify/login", protocol, ip, port)
	loginPayload := map[string]interface{}{"username": username, "password": encPassword, "keepalive": true, "is_simple": true}
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

	pubKeyBytes, _ := base64.StdEncoding.DecodeString(loginResp.Data.PublicKey)

	authInfo := &UGreenAuthInfo{
		TokenID:   loginResp.Data.TokenID,
		Token:     loginResp.Data.Token, // 修正：直接保存原始Token
		PublicKey: string(pubKeyBytes),
	}
	saveUGreenAuthInfo(ip, port, authInfo)
	return authInfo, nil
}

func fetchUGreenNotices(authInfo *UGreenAuthInfo, ip string, port int, useSSL bool) ([]UGreenNotice, int, error) {
	protocol := "http"
	if useSSL {
		protocol = "https"
	}
	url := fmt.Sprintf("%s://%s:%d/ugreen/v1/desktop/message/list", protocol, ip, port)

	payload := map[string]interface{}{"level": []string{"info", "important", "warning"}, "page": 1, "size": 10}
	reqBody, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(reqBody))

	encToken, _ := RsaEncrypt(authInfo.PublicKey, authInfo.Token)

	req.Header.Set("x-specify-language", "zh-CN")
	req.Header.Set("x-ugreen-security-key", authInfo.TokenID)
	req.Header.Set("x-ugreen-token", encToken) // 动态加密传递

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

func fetchUGreenSystemInfo(authInfo *UGreenAuthInfo, ip string, port int, useSSL bool) (*UGreenSystemInfo, error) {
	protocol := "http"
	if useSSL {
		protocol = "https"
	}
	baseURL := fmt.Sprintf("%s://%s:%d", protocol, ip, port)
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	info := &UGreenSystemInfo{}

	doGet := func(apiPath string) ([]byte, error) {
		req, _ := http.NewRequest("GET", baseURL+apiPath, nil)
		encToken, _ := RsaEncrypt(authInfo.PublicKey, authInfo.Token)

		req.Header.Set("x-specify-language", "zh-CN")
		req.Header.Set("x-ugreen-security-key", authInfo.TokenID)
		req.Header.Set("x-ugreen-token", encToken) // 动态加密传递
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		return io.ReadAll(resp.Body)
	}

	widgetsRaw, err := doGet("/ugreen/v1/desktop/components")
	if err != nil {
		widgetsRaw, _ = doGet("/ugreen/v2/desktop/components")
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
		json.Unmarshal(widgetsRaw, &wrapped)
		widgets = wrapped.Result
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
			json.Unmarshal(dataRaw, &list)
			info.Storage = list.StorageList
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

	if json.Unmarshal(raw, &respWrapper) != nil {
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

	info.NetworkReceiveValue, info.NetworkTransmitValue = recvRate, sendRate
	info.NetworkReceive, _ = formatUGreenSpeed(recvRate)
	info.NetworkTransmit, _ = formatUGreenSpeed(sendRate)
}

func formatUGreenSpeed(bytesPerSec float64) (string, string) {
	if bytesPerSec >= 1024*1024 {
		return fmt.Sprintf("%.1fMB/s", bytesPerSec/1024/1024), "MB/s"
	}
	return fmt.Sprintf("%.1fKB/s", bytesPerSec/1024), "KB/s"
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
	var maxTime int64
	for _, line := range strings.Split(string(content), "\n") {
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
	builder.WriteString(fmt.Sprintf("%s（共%d条）", typeName, len(notices)))
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
	builder.WriteString(fmt.Sprintf("📊 %s 概览报告\n", typeName))
	builder.WriteString(strings.Repeat("-", 22) + "\n")
	builder.WriteString(fmt.Sprintf("💻 CPU: %.1f%%  |  🌡️ %.1f°C\n", info.UsageCpu, info.CpuTemp))
	if info.CpuFan > 0 {
		builder.WriteString(fmt.Sprintf("🌀 CPU风扇: %d RPM\n", info.CpuFan))
	}
	if info.DeviceFan > 0 {
		builder.WriteString(fmt.Sprintf("📦 机箱风扇: %d RPM\n", info.DeviceFan))
	}
	memUsedGB := float64(info.MemoryUsed) / 1024 / 1024 / 1024
	memTotalGB := float64(info.MemoryTotal) / 1024 / 1024 / 1024
	builder.WriteString(fmt.Sprintf("🧠 内存: %.1f%% (%.1fG/%.1fG)\n", info.UsageMemory, memUsedGB, memTotalGB))
	builder.WriteString(fmt.Sprintf("🚀 下载: %s | 📤 上传: %s\n", info.NetworkReceive, info.NetworkTransmit))
	return builder.String()
}
