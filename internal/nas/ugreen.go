package nas

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"nasnotify-go/internal/config"
	"nasnotify-go/internal/crypto"
	"nasnotify-go/internal/notify"
	"nasnotify-go/internal/utils"
)

func newUGreenHTTPClient(timeout time.Duration, jar http.CookieJar) *http.Client {
	return &http.Client{
		Jar:     jar,
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
			Proxy:           http.ProxyFromEnvironment,
		},
	}
}

// UGreenAuthInfo 认证信息
type UGreenAuthInfo struct {
	TokenID   string `json:"token_id"`
	Token     string `json:"token"`
	PublicKey string `json:"public_key"`
	CookieStr string `json:"cookie_str"`
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

func configuredUGreenDevice() *config.UGreenConfig {
	cfg := config.GetConfigSnapshot()

	if local := buildLocalUGreenConfig(cfg); local != nil {
		return local
	}

	return nil
}

func buildLocalUGreenConfig(cfg config.AppConfig) *config.UGreenConfig {
	username := strings.TrimSpace(cfg.LocalNasUsername)
	if username == "" {
		return nil
	}

	name := strings.TrimSpace(cfg.LocalNasName)
	if name == "" {
		name = "本机绿联 NAS"
	}

	port := cfg.LocalNasPort
	if port <= 0 {
		port = 9999
	}

	return &config.UGreenConfig{
		ID:             "local-ugreen",
		IpPort:         fmt.Sprintf("127.0.0.1:%d", port),
		Username:       username,
		Password:       cfg.LocalNasPassword,
		NotifyTypeName: name,
		UseSSL:         false,
	}
}

func ugreenDeviceLabel(cfg config.UGreenConfig) string {
	if name := strings.TrimSpace(cfg.NotifyTypeName); name != "" {
		return name
	}
	if address := strings.TrimSpace(cfg.IpPort); address != "" {
		return address
	}
	return "未命名设备"
}

func parseUGreenPerfCommand(command string) (action, mode string, ok bool) {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return "", "", false
	}

	rawAction := fields[0]
	upperAction := strings.ToUpper(rawAction)

	switch {
	case strings.HasPrefix(rawAction, "风扇"):
		action = "风扇"
		mode = strings.TrimSpace(strings.TrimPrefix(rawAction, "风扇"))
	case strings.HasPrefix(upperAction, "FAN"):
		action = "FAN"
		mode = strings.TrimSpace(rawAction[len("FAN"):])
	case strings.HasPrefix(upperAction, "CPU"):
		action = "CPU"
		mode = strings.TrimSpace(rawAction[len("CPU"):])
	default:
		return "", "", false
	}

	if mode == "" {
		if len(fields) < 2 {
			return "", "", false
		}
		mode = strings.TrimSpace(fields[1])
	}
	return action, mode, mode != ""
}

func ProcessUGreen() {
	cfg := configuredUGreenDevice()
	if cfg == nil {
		return
	}

	ip, port := utils.SplitIpPort(cfg.IpPort, 9999)
	log.Printf("[绿联] 开始检查设备: %s (%s:%d, HTTPS=%t)\n", ugreenDeviceLabel(*cfg), ip, port, cfg.UseSSL)
	if !utils.HandleDeviceStatus("绿联", cfg.NotifyTypeName, ip, port) {
		return
	}

	logFile := utils.DeviceLogFile("ugreen", cfg.ID, ip, port)
	authInfo := ensureAuth(cfg.Username, cfg.Password, ip, port, cfg.UseSSL)
	if authInfo == nil {
		log.Printf("[绿联] %s 登录失败，未能获取认证信息\n", ugreenDeviceLabel(*cfg))
		return
	}

	notices, code, err := fetchUGreenNotices(authInfo, ip, port, cfg.UseSSL)
	if err == nil && code != 200 {
		authInfo, err = loginUGreen(cfg.Username, cfg.Password, ip, port, cfg.UseSSL)
		if err == nil {
			notices, code, err = fetchUGreenNotices(authInfo, ip, port, cfg.UseSSL)
		}
	}

	if err != nil {
		log.Printf("[绿联] %s 获取通知失败: %v\n", ugreenDeviceLabel(*cfg), err)
		return
	}
	if code != 200 {
		log.Printf("[绿联] %s 获取通知失败: API 返回代码 %d\n", ugreenDeviceLabel(*cfg), code)
		return
	}

	lastTime := getLastUGreenTime(logFile)
	var newNotices []UGreenNotice
	for _, notice := range notices {
		if notice.Time > lastTime {
			newNotices = append(newNotices, notice)
		}
	}

	fileInfo, err := os.Stat(logFile)
	isFirstRun := false
	if err != nil {
		isFirstRun = os.IsNotExist(err)
	} else {
		isFirstRun = fileInfo.Size() == 0
	}

	if isFirstRun || len(newNotices) > 0 {
		if err := saveUGreenNotices(newNotices, logFile); err != nil {
			log.Printf("[绿联] %s 保存通知日志失败: %v\n", ugreenDeviceLabel(*cfg), err)
			return
		}
		pushContent := buildUGreenPushContent(newNotices, cfg.NotifyTypeName)
		if pushContent != "" {
			notify.WechatPush(pushContent)
		}
	}
	if len(newNotices) == 0 {
		log.Printf("[绿联] %s 没有新的通知\n", ugreenDeviceLabel(*cfg))
	}
}

func PushUGreenSystemStatus() {
	if cfg := configuredUGreenDevice(); cfg != nil {
		pushUGreenSystemStatus(*cfg)
	}
}

func pushUGreenSystemStatus(cfg config.UGreenConfig) {
	ip, port := utils.SplitIpPort(cfg.IpPort, 9999)
	authInfo := ensureAuth(cfg.Username, cfg.Password, ip, port, cfg.UseSSL)
	if authInfo == nil {
		log.Printf("[绿联] %s 获取系统概览失败: 登录失败\n", ugreenDeviceLabel(cfg))
		return
	}

	info, err := fetchUGreenSystemInfo(authInfo, ip, port, cfg.UseSSL)
	if err != nil {
		authInfo = refreshUGreenAuth(cfg.Username, cfg.Password, ip, port, cfg.UseSSL)
		if authInfo != nil {
			info, err = fetchUGreenSystemInfo(authInfo, ip, port, cfg.UseSSL)
		}
	}
	if err != nil || info == nil {
		log.Printf("[绿联] %s 获取系统概览失败: %v\n", ugreenDeviceLabel(cfg), err)
		return
	}

	pushContent := buildUGreenSystemStatusPushContent(info, cfg.NotifyTypeName)
	if pushContent != "" {
		notify.WechatPush(pushContent)
	}
}

func PushUGreenStorageStatus() {
	if cfg := configuredUGreenDevice(); cfg != nil {
		pushUGreenStorageStatus(*cfg)
	}
}

func pushUGreenStorageStatus(cfg config.UGreenConfig) {
	ip, port := utils.SplitIpPort(cfg.IpPort, 9999)
	authInfo := ensureAuth(cfg.Username, cfg.Password, ip, port, cfg.UseSSL)
	if authInfo == nil {
		log.Printf("[绿联] %s 获取存储状态失败: 登录失败\n", ugreenDeviceLabel(cfg))
		return
	}

	raw, err := requestUGreenDeepAPI(authInfo, ip, port, cfg.UseSSL, "GET", "/ugreen/v1/storage/volume/list", nil, nil)
	if err != nil {
		notify.WechatPush("⚠️ 获取存储卷信息失败: " + err.Error())
		return
	}

	// 修复：对齐绿联最新的真实 JSON 字段名
	type VolumeItem struct {
		Name       string `json:"name"`
		Label      string `json:"label"`
		PoolName   string `json:"poolname"` // 修正 pool_name -> poolname
		Total      int64  `json:"total"`    // 修正 size -> total
		Used       int64  `json:"used"`
		Status     int    `json:"status"`
		FileSystem string `json:"filesystem"` // 修正 fs_type -> filesystem
	}

	var volumes []VolumeItem
	if err := json.Unmarshal(raw, &volumes); err != nil {
		var wrapped struct {
			List   []VolumeItem `json:"list"`
			Result []VolumeItem `json:"result"`
		}
		if err2 := json.Unmarshal(raw, &wrapped); err2 == nil {
			if len(wrapped.Result) > 0 {
				volumes = wrapped.Result
			} else {
				volumes = wrapped.List
			}
		}
	}

	if len(volumes) == 0 {
		notify.WechatPush("⚠️ 当前未获取到存储卷信息 (或无存储空间)")
		return
	}

	var builder strings.Builder
	builder.WriteString(wechatCardHeader("💾", "存储卷状态", cfg.NotifyTypeName))

	for i, v := range volumes {
		usagePct := 0.0
		if v.Total > 0 {
			usagePct = float64(v.Used) / float64(v.Total) * 100
		}

		label := v.Label
		if label == "" {
			label = v.Name
		}

		if i > 0 {
			builder.WriteString("\n")
		}
		builder.WriteString(fmt.Sprintf("▌%s\n", fallbackText(label, "未命名卷")))
		if v.PoolName != "" {
			builder.WriteString(fmt.Sprintf("POOL  %s\n", v.PoolName))
		}
		builder.WriteString(wechatPercentLine("USED", usagePct) + "\n")
		builder.WriteString(fmt.Sprintf("SIZE  %s / %s\n", formatBytesHuman(v.Used), formatBytesHuman(v.Total)))
		if v.FileSystem != "" {
			builder.WriteString(fmt.Sprintf("FS    %s\n", v.FileSystem))
		}
	}

	notify.WechatPush(strings.TrimSpace(builder.String()))
}

func PushUGreenUpsStatus() {
	if cfg := configuredUGreenDevice(); cfg != nil {
		pushUGreenUpsStatus(*cfg)
	}
}

func pushUGreenUpsStatus(cfg config.UGreenConfig) {
	ip, port := utils.SplitIpPort(cfg.IpPort, 9999)
	authInfo := ensureAuth(cfg.Username, cfg.Password, ip, port, cfg.UseSSL)
	if authInfo == nil {
		log.Printf("[绿联] %s 获取 UPS 状态失败: 登录失败\n", ugreenDeviceLabel(cfg))
		return
	}

	cfgRaw, cfgErr := requestUGreenDeepAPI(authInfo, ip, port, cfg.UseSSL, "GET", "/ugreen/v1/hardware/ups/config", nil, nil)
	if cfgErr != nil {
		notify.WechatPush("⚠️ 获取 UPS 配置失败: " + cfgErr.Error())
		return
	}
	usbRaw, usbErr := requestUGreenDeepAPI(authInfo, ip, port, cfg.UseSSL, "GET", "/ugreen/v1/hardware/ups/usb/info", nil, nil)
	if usbErr != nil {
		notify.WechatPush("⚠️ 获取 UPS USB 状态失败: " + usbErr.Error())
		return
	}

	type UpsInfoData struct {
		Supplier           string `json:"supplier"`
		ProductMode        string `json:"product_mode"`
		BatteryCapacity    string `json:"battery_capacity"`
		EstimateSupplyTime int    `json:"estimate_supply_time"`
	}

	type UpsCfgData struct {
		Status           bool        `json:"status"`
		StandbyTime      int         `json:"standby_time"`
		StandbyTimeUnit  int         `json:"standby_time_unit"`
		ProtectType      int         `json:"protect_type"`
		SnmpUpsConnected bool        `json:"snmp_ups_connected"`
		UpsInfo          UpsInfoData `json:"ups_info"`
	}

	var cfgData UpsCfgData
	if err := json.Unmarshal(cfgRaw, &cfgData); err != nil {
		var wrapped struct {
			Data UpsCfgData `json:"data"`
		}
		json.Unmarshal(cfgRaw, &wrapped)
		cfgData = wrapped.Data
	}

	type UsbData struct {
		Supplier     string `json:"supplier"`
		ProductMode  string `json:"product_mode"`
		UsbUpsInsert bool   `json:"usb_ups_insert"`
	}
	var usb UsbData
	if err := json.Unmarshal(usbRaw, &usb); err != nil {
		var wrapped struct {
			Data UsbData `json:"data"`
		}
		json.Unmarshal(usbRaw, &wrapped)
		usb = wrapped.Data
	}

	var builder strings.Builder
	builder.WriteString(wechatCardHeader("🔋", "UPS 电源状态", cfg.NotifyTypeName))

	if usb.UsbUpsInsert {
		builder.WriteString(fmt.Sprintf("DEVICE  %s %s (USB)\n", usb.Supplier, usb.ProductMode))
	} else if cfgData.SnmpUpsConnected {
		builder.WriteString(fmt.Sprintf("DEVICE  %s %s (SNMP)\n", cfgData.UpsInfo.Supplier, cfgData.UpsInfo.ProductMode))
	} else {
		builder.WriteString("⚠️ 当前未连接 UPS 设备或设备离线\n")
		notify.WechatPush(builder.String())
		return
	}

	statusStr := "已停止 ❌"
	if cfgData.Status {
		statusStr = "运行中 ✅"
	}
	builder.WriteString(fmt.Sprintf("STATE   %s\n", statusStr))

	cap := cfgData.UpsInfo.BatteryCapacity
	capPct := -1.0
	if cap == "" {
		cap = "未知"
	} else {
		if parsed, err := strconv.ParseFloat(cap, 64); err == nil {
			capPct = parsed
		}
		cap += "%"
	}
	builder.WriteString(fmt.Sprintf("BATTERY %s\n", cap))
	if capPct >= 0 {
		builder.WriteString(wechatPercentLine("POWER", capPct) + "\n")
	}

	est := cfgData.UpsInfo.EstimateSupplyTime
	if est < 0 {
		builder.WriteString("INPUT   市电供电中 ⚡\n")
	} else if est == 0 {
		builder.WriteString("RUNTIME 正在计算中...\n")
	} else {
		builder.WriteString(fmt.Sprintf("RUNTIME %d秒 (约%.1f分钟)\n", est, float64(est)/60))
	}

	protectType := "未知"
	switch cfgData.ProtectType {
	case 0:
		protectType = "不保护"
	case 1:
		protectType = "安全关机"
	case 2:
		protectType = "进入待机"
	}
	builder.WriteString(fmt.Sprintf("POLICY  %s\n", protectType))

	if cfgData.StandbyTime > 0 {
		unit := "秒"
		if cfgData.StandbyTimeUnit == 1 {
			unit = "分钟"
		}
		builder.WriteString(fmt.Sprintf("DELAY   %d %s后执行保护\n", cfgData.StandbyTime, unit))
	}

	notify.WechatPush(strings.TrimSpace(builder.String()))
}

func PushUGreenNotifyStatus() {
	if cfg := configuredUGreenDevice(); cfg != nil {
		pushUGreenNotifyStatus(*cfg)
	}
}

func pushUGreenNotifyStatus(cfg config.UGreenConfig) {
	ip, port := utils.SplitIpPort(cfg.IpPort, 9999)
	authInfo := ensureAuth(cfg.Username, cfg.Password, ip, port, cfg.UseSSL)
	if authInfo == nil {
		log.Printf("[绿联] %s 获取系统通知失败: 登录失败\n", ugreenDeviceLabel(cfg))
		return
	}

	notices, code, err := fetchUGreenNotices(authInfo, ip, port, cfg.UseSSL)
	if err != nil || code != 200 {
		log.Printf("[绿联] %s 获取系统通知失败: code=%d, err=%v\n", ugreenDeviceLabel(cfg), code, err)
		return
	}
	if len(notices) > 0 {
		notify.WechatPush(buildUGreenPushContent(notices, cfg.NotifyTypeName+" 近期通知"))
	} else {
		notify.WechatPush(fmt.Sprintf("%s 当前没有新的系统通知。", cfg.NotifyTypeName))
	}
}

func PushUGreenDockerStatus() {
	if cfg := configuredUGreenDevice(); cfg != nil {
		pushUGreenDockerStatus(*cfg)
	}
}

func pushUGreenDockerStatus(cfg config.UGreenConfig) {
	ip, port := utils.SplitIpPort(cfg.IpPort, 9999)
	authInfo := ensureAuth(cfg.Username, cfg.Password, ip, port, cfg.UseSSL)
	if authInfo == nil {
		log.Printf("[绿联] %s 获取 Docker 状态失败: 登录失败\n", ugreenDeviceLabel(cfg))
		return
	}

	ovRaw, err := requestUGreenDeepAPI(authInfo, ip, port, cfg.UseSSL, "GET", "/ugreen/v1/docker/view/ObtainOverviewInfo", nil, nil)
	if err != nil {
		notify.WechatPush("⚠️ 获取 Docker 状态失败: " + err.Error())
		return
	}
	var overview struct {
		RunContainerCount int `json:"runContainerCount"`
		ContainerCount    int `json:"containerCount"`
		ImageCount        int `json:"imageCount"`
		CpuUsed           int `json:"cpuUsed"`
	}
	json.Unmarshal(ovRaw, &overview)

	listRaw, _ := requestUGreenDeepAPI(authInfo, ip, port, cfg.UseSSL, "POST", "/ugreen/v1/docker/container/ContainerListV2", nil, map[string]interface{}{"pageNum": 1, "pageSize": 200})
	var list struct {
		Result []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"result"`
	}
	json.Unmarshal(listRaw, &list)

	var builder strings.Builder
	builder.WriteString(wechatCardHeader("🐳", "Docker 运行概览", cfg.NotifyTypeName))
	builder.WriteString(wechatCountLine("RUN", overview.RunContainerCount, overview.ContainerCount) + "\n")
	builder.WriteString(wechatPercentLine("LOAD", float64(overview.CpuUsed)) + "\n")
	builder.WriteString(fmt.Sprintf("IMAGE  %d\n", overview.ImageCount))

	builder.WriteString(wechatSection("Container Matrix"))
	count := 0
	for _, c := range list.Result {
		if c.Status == "running" || c.Status == "Up" {
			builder.WriteString(fmt.Sprintf("%2d. %s\n", count+1, trimDisplayText(c.Name, 26)))
			count++
			if count >= 10 {
				break
			}
		}
	}
	if count == 0 {
		builder.WriteString("当前无运行中容器\n")
	} else if overview.RunContainerCount > count {
		builder.WriteString(fmt.Sprintf("...另有 %d 个运行中容器\n", overview.RunContainerCount-count))
	}

	notify.WechatPush(strings.TrimSpace(builder.String()))
}

func PushUGreenPsStatus() {
	if cfg := configuredUGreenDevice(); cfg != nil {
		pushUGreenPsStatus(*cfg)
	}
}

func pushUGreenPsStatus(cfg config.UGreenConfig) {
	ip, port := utils.SplitIpPort(cfg.IpPort, 9999)
	authInfo := ensureAuth(cfg.Username, cfg.Password, ip, port, cfg.UseSSL)
	if authInfo == nil {
		log.Printf("[绿联] %s 获取进程列表失败: 登录失败\n", ugreenDeviceLabel(cfg))
		return
	}

	raw, err := requestUGreenDeepAPI(authInfo, ip, port, cfg.UseSSL, "GET", "/ugreen/v1/taskmgr/services/processes", nil, nil)
	if err != nil {
		notify.WechatPush("⚠️ 获取进程列表失败: " + err.Error())
		return
	}

	type ProcessItem struct {
		Name    string `json:"name"`
		Consume struct {
			CPU    float64 `json:"cpu_used_percent"`
			Memory float64 `json:"mem_used_percent"`
		} `json:"consume"`
	}

	var resp struct {
		Services struct {
			List []ProcessItem `json:"list"`
		} `json:"services"`
		Processes struct {
			List []ProcessItem `json:"list"`
		} `json:"processes"`
	}
	json.Unmarshal(raw, &resp)

	allProcs := append(resp.Services.List, resp.Processes.List...)

	sort.Slice(allProcs, func(i, j int) bool {
		return allProcs[i].Consume.CPU > allProcs[j].Consume.CPU
	})

	var builder strings.Builder
	builder.WriteString(wechatCardHeader("📈", "进程占用 TOP 5", cfg.NotifyTypeName))
	if len(allProcs) == 0 {
		builder.WriteString("当前未获取到进程数据\n")
		notify.WechatPush(strings.TrimSpace(builder.String()))
		return
	}

	for i, p := range allProcs {
		if i >= 5 {
			break
		}
		builder.WriteString(fmt.Sprintf("%d. %s\n", i+1, trimDisplayText(p.Name, 24)))
		builder.WriteString(fmt.Sprintf("   CPU %5.1f%% [%s]\n", clampPercent(p.Consume.CPU), wechatProgressBar(p.Consume.CPU, 10)))
		builder.WriteString(fmt.Sprintf("   MEM %5.1f%% [%s]\n", clampPercent(p.Consume.Memory), wechatProgressBar(p.Consume.Memory, 10)))
	}

	notify.WechatPush(strings.TrimSpace(builder.String()))
}

func PushUGreenBackupStatus() {
	if cfg := configuredUGreenDevice(); cfg != nil {
		pushUGreenBackupStatus(*cfg)
	}
}

func pushUGreenBackupStatus(cfg config.UGreenConfig) {
	ip, port := utils.SplitIpPort(cfg.IpPort, 9999)
	authInfo := ensureAuth(cfg.Username, cfg.Password, ip, port, cfg.UseSSL)
	if authInfo == nil {
		log.Printf("[绿联] %s 获取备份任务失败: 登录失败\n", ugreenDeviceLabel(cfg))
		return
	}

	raw, err := requestUGreenDeepAPI(authInfo, ip, port, cfg.UseSSL, "GET", "/ugreen/v2/web/syncbackup/task/list", map[string]string{"backup_type": "backup", "page": "1", "size": "100"}, nil)
	if err != nil {
		notify.WechatPush("⚠️ 获取备份任务失败: " + err.Error())
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
	builder.WriteString(wechatCardHeader("🔄", "备份任务状态", cfg.NotifyTypeName))
	if len(result.List) == 0 {
		builder.WriteString("当前没有配置备份任务\n")
	} else {
		builder.WriteString(fmt.Sprintf("TASKS  %d\n", len(result.List)))
		for i, t := range result.List {
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
			if i > 0 {
				builder.WriteString("\n")
			}
			builder.WriteString(fmt.Sprintf("%d. %s\n", i+1, trimDisplayText(t.TaskName, 28)))
			builder.WriteString(fmt.Sprintf("   STATE %s\n", statusStr))
			builder.WriteString(fmt.Sprintf("   SYNC  %s\n", lastSync))
		}
	}
	notify.WechatPush(strings.TrimSpace(builder.String()))
}

func PushUGreenPowerStatus() {
	if cfg := configuredUGreenDevice(); cfg != nil {
		pushUGreenPowerStatus(*cfg)
	}
}

func pushUGreenPowerStatus(cfg config.UGreenConfig) {
	ip, port := utils.SplitIpPort(cfg.IpPort, 9999)
	authInfo := ensureAuth(cfg.Username, cfg.Password, ip, port, cfg.UseSSL)
	if authInfo == nil {
		log.Printf("[绿联] %s 获取电源配置失败: 登录失败\n", ugreenDeviceLabel(cfg))
		return
	}

	raw, err := requestUGreenDeepAPI(authInfo, ip, port, cfg.UseSSL, "GET", "/ugreen/v1/hardware/power/config", nil, nil)
	if err != nil {
		notify.WechatPush("⚠️ 获取电源配置失败: " + err.Error())
		return
	}

	var cfgData struct {
		PowerBoot     bool   `json:"power_boot"`
		WakeOn        bool   `json:"wake_on"`
		HardDriveFlag bool   `json:"hard_drive_flag"`
		HardDriveTime int    `json:"hard_drive_time"`
		HardDriveUnit string `json:"hard_drive_unit"`
	}
	if err := json.Unmarshal(raw, &cfgData); err != nil {
		var wrapped struct {
			Data struct {
				PowerBoot     bool   `json:"power_boot"`
				WakeOn        bool   `json:"wake_on"`
				HardDriveFlag bool   `json:"hard_drive_flag"`
				HardDriveTime int    `json:"hard_drive_time"`
				HardDriveUnit string `json:"hard_drive_unit"`
			} `json:"data"`
		}
		json.Unmarshal(raw, &wrapped)
		cfgData = wrapped.Data
	}

	var builder strings.Builder
	builder.WriteString(wechatCardHeader("⚡", "电源与休眠配置", cfg.NotifyTypeName))

	statusMap := func(b bool) string {
		return enabledStatus(b)
	}

	unitMap := func(u string) string {
		if u == "H" {
			return "小时"
		}
		return "分钟"
	}

	builder.WriteString(fmt.Sprintf("BOOT  %s\n", statusMap(cfgData.PowerBoot)))
	builder.WriteString(fmt.Sprintf("WOL   %s\n", statusMap(cfgData.WakeOn)))
	builder.WriteString(fmt.Sprintf("SLEEP %s\n", statusMap(cfgData.HardDriveFlag)))
	if cfgData.HardDriveFlag {
		builder.WriteString(fmt.Sprintf("TIMER %d %s\n", cfgData.HardDriveTime, unitMap(cfgData.HardDriveUnit)))
	}

	notify.WechatPush(strings.TrimSpace(builder.String()))
}

func HandleUGreenPerfCommand(command string) {
	cfg := configuredUGreenDevice()
	if cfg == nil {
		return
	}

	action, modeStr, ok := parseUGreenPerfCommand(command)
	if !ok {
		notify.WechatPush("⚠️ 指令格式错误，请发送类似：风扇2、风扇 2、CPU1、CPU 1")
		return
	}

	ip, port := utils.SplitIpPort(cfg.IpPort, 9999)
	authInfo := ensureAuth(cfg.Username, cfg.Password, ip, port, cfg.UseSSL)
	if authInfo == nil {
		notify.WechatPush(fmt.Sprintf("❌ %s 控制指令失败：登录凭证无效", ugreenDeviceLabel(*cfg)))
		return
	}

	var successMsg string
	var execErr error

	if action == "风扇" || action == "FAN" {
		mode, _ := strconv.Atoi(modeStr)
		if mode < 1 || mode > 3 {
			notify.WechatPush("⚠️ 风扇指令格式错误。\n1: 静音 | 2: 正常 | 3: 全速\n例如: 风扇2")
			return
		}
		_, execErr = requestUGreenDeepAPI(authInfo, ip, port, cfg.UseSSL, "GET", "/ugreen/v1/hardware/fan/start", map[string]string{"mode": strconv.Itoa(mode)}, nil)
		modes := map[int]string{1: "静音", 2: "正常", 3: "全速"}
		successMsg = fmt.Sprintf("🌀 %s 风扇模式已成功切换为: %s", ugreenDeviceLabel(*cfg), modes[mode])
	} else if action == "CPU" {
		mode, _ := strconv.Atoi(modeStr)
		if mode < 0 || mode > 2 {
			notify.WechatPush("⚠️ CPU指令格式错误。\n0: 高性能 | 1: 均衡 | 2: 节能\n例如: CPU1")
			return
		}
		_, execErr = requestUGreenDeepAPI(authInfo, ip, port, cfg.UseSSL, "POST", "/ugreen/v1/hardware/cpu/frequency", nil, map[string]interface{}{"frequency": mode})
		modes := map[int]string{0: "高性能", 1: "均衡", 2: "节能"}
		successMsg = fmt.Sprintf("⚡ %s CPU 模式已成功切换为: %s", ugreenDeviceLabel(*cfg), modes[mode])
	} else {
		notify.WechatPush("⚠️ 指令类型不支持，请发送“风扇 ...”或“CPU ...”")
		return
	}

	if execErr != nil {
		notify.WechatPush(fmt.Sprintf("❌ %s 指令执行失败: %v", ugreenDeviceLabel(*cfg), execErr))
	} else {
		notify.WechatPush(successMsg)
	}
}

func requestUGreenDeepAPI(authInfo *UGreenAuthInfo, ip string, port int, useSSL bool, method string, apiPath string, params map[string]string, body map[string]interface{}) ([]byte, error) {
	protocol := "http"
	if useSSL {
		protocol = "https"
	}

	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	aesKey := hex.EncodeToString(b)

	urlStr := fmt.Sprintf("%s://%s:%d%s", protocol, ip, port, apiPath)

	if len(params) > 0 {
		q := url.Values{}
		for k, v := range params {
			q.Set(k, v)
		}
		encQuery, err := crypto.AESGCMEncrypt(aesKey, q.Encode())
		if err != nil {
			return nil, err
		}
		urlStr += "?encrypt_query=" + url.QueryEscape(encQuery)
	}

	var bodyReader io.Reader
	if body != nil {
		bodyJSON, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		encBody, err := crypto.AESGCMEncrypt(aesKey, string(bodyJSON))
		if err != nil {
			return nil, err
		}
		encReq := map[string]string{"encrypt_req_body": encBody}
		encReqJSON, err := json.Marshal(encReq)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(encReqJSON)
	}

	req, err := http.NewRequest(method, urlStr, bodyReader)
	if err != nil {
		return nil, err
	}

	securityCode, err := crypto.RsaEncrypt(authInfo.PublicKey, aesKey)
	if err != nil {
		return nil, err
	}
	ugreenToken, err := crypto.RsaEncrypt(authInfo.PublicKey, authInfo.Token)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Client-Id", "cli-go-tool")
	req.Header.Set("Client-Version", "77682")
	req.Header.Set("UG-Agent", "PC/WEB")
	req.Header.Set("X-Specify-Language", "zh-CN")
	req.Header.Set("X-Ugreen-Security-Code", securityCode)
	req.Header.Set("X-Ugreen-Security-Key", crypto.MD5Hex(authInfo.Token))
	req.Header.Set("X-Ugreen-Token", ugreenToken)

	if authInfo.CookieStr != "" {
		req.Header.Set("Cookie", authInfo.CookieStr)
	}

	client := newUGreenHTTPClient(10*time.Second, nil)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("api http status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	rawStr := string(raw)

	decrypted, err := crypto.AESGCMDecrypt(aesKey, rawStr)
	if err == nil {
		var apiResp struct {
			Code int             `json:"code"`
			Msg  string          `json:"msg"`
			Data json.RawMessage `json:"data"`
		}
		if jsonErr := json.Unmarshal([]byte(decrypted), &apiResp); jsonErr == nil {
			if apiResp.Code != 200 && apiResp.Code != 0 {
				return nil, fmt.Errorf("api error: %v, %s", apiResp.Code, apiResp.Msg)
			}
			if len(apiResp.Data) > 0 {
				return apiResp.Data, nil
			}
		}
		return []byte(decrypted), nil
	}

	var encResp struct {
		EncryptRespBody string `json:"encrypt_resp_body"`
	}
	if json.Unmarshal(raw, &encResp) == nil && encResp.EncryptRespBody != "" {
		dec, decErr := crypto.AESGCMDecrypt(aesKey, encResp.EncryptRespBody)
		if decErr == nil {
			var apiResp struct {
				Code int             `json:"code"`
				Msg  string          `json:"msg"`
				Data json.RawMessage `json:"data"`
			}
			if jsonErr := json.Unmarshal([]byte(dec), &apiResp); jsonErr == nil {
				if apiResp.Code != 200 && apiResp.Code != 0 {
					return nil, fmt.Errorf("api error: %v, %s", apiResp.Code, apiResp.Msg)
				}
				if len(apiResp.Data) > 0 {
					return apiResp.Data, nil
				}
			}
			return []byte(dec), nil
		}
	}

	var apiResp struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &apiResp); err == nil {
		if apiResp.Code != 200 && apiResp.Code != 0 {
			return nil, fmt.Errorf("api error: %v, %s", apiResp.Code, apiResp.Msg)
		}
		var dataFields struct {
			EncryptRespBody string `json:"encrypt_resp_body"`
		}
		if json.Unmarshal(apiResp.Data, &dataFields) == nil && dataFields.EncryptRespBody != "" {
			dec, decErr := crypto.AESGCMDecrypt(aesKey, dataFields.EncryptRespBody)
			if decErr == nil {
				return []byte(dec), nil
			}
		}
		if len(apiResp.Data) > 0 {
			return apiResp.Data, nil
		}
	}

	return nil, fmt.Errorf("failed to parse response")
}

func ensureAuth(username, password, ip string, port int, useSSL bool) *UGreenAuthInfo {
	authInfo := loadUGreenAuthInfo(ip, port)
	if authInfo != nil && authInfo.PublicKey != "" && authInfo.CookieStr != "" {
		return authInfo
	}
	newAuth, err := loginUGreen(username, password, ip, port, useSSL)
	if err != nil {
		log.Printf("[绿联] %s:%d 登录失败: %v\n", ip, port, err)
	}
	return newAuth
}

func refreshUGreenAuth(username, password, ip string, port int, useSSL bool) *UGreenAuthInfo {
	newAuth, err := loginUGreen(username, password, ip, port, useSSL)
	if err != nil {
		log.Printf("[ugreen] %s:%d re-login failed: %v\n", ip, port, err)
		return nil
	}
	return newAuth
}

func loginUGreen(username, password, ip string, port int, useSSL bool) (*UGreenAuthInfo, error) {
	protocol := "http"
	if useSSL {
		protocol = "https"
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	client := newUGreenHTTPClient(10*time.Second, jar)

	encPassword := password
	if username != "admin" {
		checkURL := fmt.Sprintf("%s://%s:%d/ugreen/v1/verify/check", protocol, ip, port)
		checkReqBody, err := json.Marshal(map[string]string{"username": username})
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequest("POST", checkURL, bytes.NewBuffer(checkReqBody))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")

		if resp, err := client.Do(req); err == nil {
			rsaToken := resp.Header.Get("x-rsa-token")
			resp.Body.Close()
			if rsaToken != "" {
				if pemBytes, err := base64.StdEncoding.DecodeString(rsaToken); err == nil {
					if enc, err := crypto.RsaEncrypt(string(pemBytes), password); err == nil {
						encPassword = enc
					}
				}
			}
		}
	}

	loginURL := fmt.Sprintf("%s://%s:%d/ugreen/v1/verify/login", protocol, ip, port)
	loginPayload := map[string]interface{}{"username": username, "password": encPassword, "keepalive": true, "is_simple": true, "otp": false}
	loginReqBody, err := json.Marshal(loginPayload)
	if err != nil {
		return nil, err
	}
	req2, err := http.NewRequest("POST", loginURL, bytes.NewBuffer(loginReqBody))
	if err != nil {
		return nil, err
	}
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Client-Id", "cli-go-tool")
	req2.Header.Set("Client-Version", "77682")
	req2.Header.Set("UG-Agent", "PC/WEB")
	req2.Header.Set("x-specify-language", "zh-CN")

	resp2, err := client.Do(req2)
	if err != nil {
		return nil, err
	}
	defer resp2.Body.Close()

	body2, err := io.ReadAll(resp2.Body)
	if err != nil {
		return nil, err
	}
	if resp2.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("login http status %d: %s", resp2.StatusCode, strings.TrimSpace(string(body2)))
	}
	var loginResp UGreenLoginResp
	if err := json.Unmarshal(body2, &loginResp); err != nil {
		return nil, err
	}
	if loginResp.Code != 200 {
		return nil, fmt.Errorf("login failed: code %d", loginResp.Code)
	}

	pubKeyBytes, err := base64.StdEncoding.DecodeString(loginResp.Data.PublicKey)
	if err != nil {
		return nil, err
	}

	u, _ := url.Parse(fmt.Sprintf("%s://%s:%d/ugreen/", protocol, ip, port))
	var cookiePairs []string
	for _, c := range jar.Cookies(u) {
		cookiePairs = append(cookiePairs, c.Name+"="+c.Value)
	}
	cookieStr := strings.Join(cookiePairs, "; ")

	authInfo := &UGreenAuthInfo{
		TokenID:   loginResp.Data.TokenID,
		Token:     loginResp.Data.Token,
		PublicKey: string(pubKeyBytes),
		CookieStr: cookieStr,
	}
	if err := saveUGreenAuthInfo(ip, port, authInfo); err != nil {
		log.Printf("[ugreen] save auth cache failed for %s:%d: %v", ip, port, err)
	}
	return authInfo, nil
}

func fetchUGreenNotices(authInfo *UGreenAuthInfo, ip string, port int, useSSL bool) ([]UGreenNotice, int, error) {
	protocol := "http"
	if useSSL {
		protocol = "https"
	}
	urlStr := fmt.Sprintf("%s://%s:%d/ugreen/v1/desktop/message/list", protocol, ip, port)

	payload := map[string]interface{}{"level": []string{"info", "important", "warning"}, "page": 1, "size": 10}
	reqBody, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequest("POST", urlStr, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, 0, err
	}

	encToken, err := crypto.RsaEncrypt(authInfo.PublicKey, authInfo.Token)
	if err != nil {
		return nil, 0, err
	}

	req.Header.Set("x-specify-language", "zh-CN")
	req.Header.Set("x-ugreen-security-key", authInfo.TokenID)
	req.Header.Set("x-ugreen-token", encToken)
	if authInfo.CookieStr != "" {
		req.Header.Set("Cookie", authInfo.CookieStr)
	}

	client := newUGreenHTTPClient(10*time.Second, nil)
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, 0, fmt.Errorf("notice http status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var listResp UGreenListResp
	if err := json.Unmarshal(body, &listResp); err != nil {
		return nil, 0, err
	}

	return listResp.Data.List, listResp.Code, nil
}

func fetchUGreenSystemInfo(authInfo *UGreenAuthInfo, ip string, port int, useSSL bool) (*UGreenSystemInfo, error) {
	protocol := "http"
	if useSSL {
		protocol = "https"
	}
	baseURL := fmt.Sprintf("%s://%s:%d", protocol, ip, port)
	client := newUGreenHTTPClient(10*time.Second, nil)
	info := &UGreenSystemInfo{}

	doGet := func(apiPath string) ([]byte, error) {
		req, err := http.NewRequest("GET", baseURL+apiPath, nil)
		if err != nil {
			return nil, err
		}
		encToken, err := crypto.RsaEncrypt(authInfo.PublicKey, authInfo.Token)
		if err != nil {
			return nil, err
		}

		req.Header.Set("x-specify-language", "zh-CN")
		req.Header.Set("x-ugreen-security-key", authInfo.TokenID)
		req.Header.Set("x-ugreen-token", encToken)
		if authInfo.CookieStr != "" {
			req.Header.Set("Cookie", authInfo.CookieStr)
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode >= http.StatusBadRequest {
			return nil, fmt.Errorf("api http status %d", resp.StatusCode)
		}

		var apiResp struct {
			Code int             `json:"code"`
			Msg  string          `json:"msg"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(raw, &apiResp); err == nil && (apiResp.Code != 0 || len(apiResp.Data) > 0 || apiResp.Msg != "") {
			if apiResp.Code != 0 && apiResp.Code != 200 {
				return nil, fmt.Errorf("api error: %d %s", apiResp.Code, apiResp.Msg)
			}
			if len(apiResp.Data) > 0 {
				return apiResp.Data, nil
			}
		}
		return raw, nil
	}

	widgetsRaw, err := doGet("/ugreen/v1/desktop/components")
	if err != nil {
		widgetsRaw, err = doGet("/ugreen/v2/desktop/components")
		if err != nil {
			return nil, err
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
		json.Unmarshal(widgetsRaw, &wrapped)
		widgets = wrapped.Result
	}
	if len(widgets) == 0 {
		return nil, fmt.Errorf("desktop components response is empty or invalid")
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
	type statPoint struct {
		UsedPercent float64 `json:"used_percent"`
		Temp        float64 `json:"temp"`
		RecvRate    float64 `json:"recv_rate"`
		SendRate    float64 `json:"send_rate"`
		Speed       int     `json:"speed"`
	}
	type statData struct {
		Overview struct {
			CPU       []statPoint `json:"cpu"`
			Mem       []statPoint `json:"mem"`
			Net       []statPoint `json:"net"`
			CpuFan    []statPoint `json:"cpu_fan"`
			DeviceFan []statPoint `json:"device_fan"`
		} `json:"overview"`
		CPU struct {
			Series []statPoint `json:"series"`
		} `json:"cpu"`
		Mem struct {
			Series    []statPoint `json:"series"`
			Structure struct {
				Used  int64 `json:"used"`
				Total int64 `json:"total"`
			} `json:"structure"`
		} `json:"mem"`
		Net struct {
			Series []statPoint `json:"series"`
		} `json:"net"`
	}
	hasData := func(data statData) bool {
		return len(data.Overview.CPU) > 0 ||
			len(data.Overview.Mem) > 0 ||
			len(data.Overview.Net) > 0 ||
			len(data.Overview.CpuFan) > 0 ||
			len(data.Overview.DeviceFan) > 0 ||
			len(data.CPU.Series) > 0 ||
			len(data.Mem.Series) > 0 ||
			len(data.Net.Series) > 0 ||
			data.Mem.Structure.Used > 0 ||
			data.Mem.Structure.Total > 0
	}

	var wrapped struct {
		Data statData `json:"data"`
	}
	var data statData
	if json.Unmarshal(raw, &wrapped) == nil && hasData(wrapped.Data) {
		data = wrapped.Data
	} else if json.Unmarshal(raw, &data) != nil {
		return
	}

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
	file := ugreenAuthInfoPath(ip, port)
	data, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	var auth UGreenAuthInfo
	if err := json.Unmarshal(data, &auth); err != nil {
		log.Printf("[ugreen] parse auth cache failed for %s:%d: %v", ip, port, err)
		return nil
	}
	return &auth
}

func saveUGreenAuthInfo(ip string, port int, auth *UGreenAuthInfo) error {
	file := ugreenAuthInfoPath(ip, port)
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(auth)
	if err != nil {
		return err
	}
	return os.WriteFile(file, data, 0600)
}

func ugreenAuthInfoPath(ip string, port int) string {
	dataDir := strings.TrimSpace(os.Getenv("UGAPP_DATA_DIR"))
	if dataDir == "" {
		dataDir = "data"
	}
	return filepath.Join(dataDir, "token", fmt.Sprintf("%s_%d.config", ip, port))
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

func saveUGreenNotices(notices []UGreenNotice, file string) error {
	var builder strings.Builder
	for _, notice := range notices {
		t := time.Unix(notice.Time, 0).In(time.FixedZone("CST", 8*3600))
		builder.WriteString(fmt.Sprintf("%s：%s\n", t.Format("2006-01-02 15:04:05"), notice.Body))
	}
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		return err
	}
	return os.WriteFile(file, []byte(builder.String()), 0600)
}

func buildUGreenPushContent(notices []UGreenNotice, typeName string) string {
	if len(notices) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString(wechatCardHeader("🔔", "系统通知", typeName))
	builder.WriteString(fmt.Sprintf("EVENTS %d\n", len(notices)))
	builder.WriteString(wechatSection("Event Stream"))
	for i, notice := range notices {
		builder.WriteString(fmt.Sprintf("%d. %s\n", i+1, trimDisplayText(notice.Body, 80)))
	}
	return strings.TrimSpace(builder.String())
}

func buildUGreenSystemStatusPushContent(info *UGreenSystemInfo, typeName string) string {
	if info == nil {
		return ""
	}
	var builder strings.Builder
	builder.WriteString(wechatCardHeader("📊", "系统状态概览", typeName))
	builder.WriteString(wechatPercentLine("CPU", info.UsageCpu))
	if info.CpuTemp > 0 {
		builder.WriteString(fmt.Sprintf("  TEMP %.1f°C", info.CpuTemp))
	}
	builder.WriteString("\n")

	if info.CpuFan > 0 {
		builder.WriteString(fmt.Sprintf("FAN-CPU %d RPM\n", info.CpuFan))
	}
	if info.DeviceFan > 0 {
		builder.WriteString(fmt.Sprintf("FAN-SYS %d RPM\n", info.DeviceFan))
	}
	memUsedGB := float64(info.MemoryUsed) / 1024 / 1024 / 1024
	memTotalGB := float64(info.MemoryTotal) / 1024 / 1024 / 1024
	builder.WriteString(wechatPercentLine("MEM", info.UsageMemory))
	if info.MemoryTotal > 0 {
		builder.WriteString(fmt.Sprintf("  %.1fG/%.1fG", memUsedGB, memTotalGB))
	}
	builder.WriteString("\n")

	builder.WriteString(wechatSection("Network Stream"))
	builder.WriteString(fmt.Sprintf("DOWN  %s\n", fallbackText(info.NetworkReceive, "0 KB/s")))
	builder.WriteString(fmt.Sprintf("UP    %s\n", fallbackText(info.NetworkTransmit, "0 KB/s")))

	if len(info.Storage) > 0 {
		builder.WriteString(wechatSection("Storage Radar"))
		for i, item := range info.Storage {
			if i >= 3 {
				builder.WriteString(fmt.Sprintf("...另有 %d 个存储项\n", len(info.Storage)-i))
				break
			}
			total := item.Size
			used := item.Used
			usagePct := 0.0
			if total > 0 {
				usagePct = float64(used) / float64(total) * 100
			}
			name := fallbackText(item.StorageName, fallbackText(item.Name, "未命名存储"))
			builder.WriteString(fmt.Sprintf("%s\n", trimDisplayText(name, 24)))
			builder.WriteString(wechatPercentLine("USED", usagePct) + "\n")
		}
	}

	return strings.TrimSpace(builder.String())
}
