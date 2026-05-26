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

func ProcessUGreen() {
	if len(config.Config.UGreen) == 0 {
		return
	}

	for _, cfg := range config.Config.UGreen {
		ip, port := utils.SplitIpPort(cfg.IpPort, 9999)
		if !utils.HandleDeviceStatus("绿联", cfg.NotifyTypeName, ip, port) {
			continue
		}

		logFile := filepath.Join("data", "log", fmt.Sprintf("%s_%d.log", ip, port))
		authInfo := ensureAuth(cfg.Username, cfg.Password, ip, port, cfg.UseSSL)
		if authInfo == nil {
			continue
		}

		notices, code, err := fetchUGreenNotices(authInfo, ip, port, cfg.UseSSL)
		if err == nil && code != 200 {
			authInfo, err = loginUGreen(cfg.Username, cfg.Password, ip, port, cfg.UseSSL)
			if err == nil {
				notices, _, err = fetchUGreenNotices(authInfo, ip, port, cfg.UseSSL)
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

		if isFirstRun || len(newNotices) > 0 {
			saveUGreenNotices(newNotices, logFile)
			pushContent := buildUGreenPushContent(newNotices, cfg.NotifyTypeName)
			if pushContent != "" {
				notify.WechatPush(pushContent)
			}
		}
	}
}

func PushUGreenSystemStatus() {
	if len(config.Config.UGreen) == 0 {
		return
	}
	cfg := config.Config.UGreen[0]
	ip, port := utils.SplitIpPort(cfg.IpPort, 9999)
	authInfo := ensureAuth(cfg.Username, cfg.Password, ip, port, cfg.UseSSL)
	if authInfo == nil {
		return
	}

	info, err := fetchUGreenSystemInfo(authInfo, ip, port, cfg.UseSSL)
	if err != nil {
		authInfo = ensureAuth(cfg.Username, cfg.Password, ip, port, cfg.UseSSL)
		info, _ = fetchUGreenSystemInfo(authInfo, ip, port, cfg.UseSSL)
	}

	pushContent := buildUGreenSystemStatusPushContent(info, cfg.NotifyTypeName)
	if pushContent != "" {
		notify.WechatPush(pushContent)
	}
}

func PushUGreenStorageStatus() {
	if len(config.Config.UGreen) == 0 {
		return
	}
	cfg := config.Config.UGreen[0]
	ip, port := utils.SplitIpPort(cfg.IpPort, 9999)
	authInfo := ensureAuth(cfg.Username, cfg.Password, ip, port, cfg.UseSSL)
	if authInfo == nil {
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
	builder.WriteString(fmt.Sprintf("💾 %s 存储卷状态详情\n", cfg.NotifyTypeName))
	builder.WriteString(strings.Repeat("-", 22) + "\n")

	for _, v := range volumes {
		var usedStr, totalStr string
		// 统一处理 TB 和 GB 的显示逻辑
		if v.Total >= 1024*1024*1024*1024 {
			usedStr = fmt.Sprintf("%.2f TB", float64(v.Used)/1024/1024/1024/1024)
			totalStr = fmt.Sprintf("%.2f TB", float64(v.Total)/1024/1024/1024/1024)
		} else {
			usedStr = fmt.Sprintf("%.2f GB", float64(v.Used)/1024/1024/1024)
			totalStr = fmt.Sprintf("%.2f GB", float64(v.Total)/1024/1024/1024)
		}

		usagePct := float64(0)
		if v.Total > 0 {
			usagePct = float64(v.Used) / float64(v.Total) * 100
		}

		label := v.Label
		if label == "" {
			label = v.Name
		}

		builder.WriteString(fmt.Sprintf("🔹 %s (%s)\n", label, v.PoolName))
		builder.WriteString(fmt.Sprintf("容量: %s / %s (%.1f%%)\n", usedStr, totalStr, usagePct))
		builder.WriteString(fmt.Sprintf("文件系统: %s\n\n", v.FileSystem))
	}

	notify.WechatPush(strings.TrimSpace(builder.String()))
}

func PushUGreenUpsStatus() {
	if len(config.Config.UGreen) == 0 {
		return
	}
	cfg := config.Config.UGreen[0]
	ip, port := utils.SplitIpPort(cfg.IpPort, 9999)
	authInfo := ensureAuth(cfg.Username, cfg.Password, ip, port, cfg.UseSSL)
	if authInfo == nil {
		return
	}

	cfgRaw, _ := requestUGreenDeepAPI(authInfo, ip, port, cfg.UseSSL, "GET", "/ugreen/v1/hardware/ups/config", nil, nil)
	usbRaw, _ := requestUGreenDeepAPI(authInfo, ip, port, cfg.UseSSL, "GET", "/ugreen/v1/hardware/ups/usb/info", nil, nil)

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
	builder.WriteString("🔋 UPS 电源状态\n")
	builder.WriteString(strings.Repeat("-", 22) + "\n")

	if usb.UsbUpsInsert {
		builder.WriteString(fmt.Sprintf("设备: %s %s (USB)\n", usb.Supplier, usb.ProductMode))
	} else if cfgData.SnmpUpsConnected {
		builder.WriteString(fmt.Sprintf("设备: %s %s (SNMP)\n", cfgData.UpsInfo.Supplier, cfgData.UpsInfo.ProductMode))
	} else {
		builder.WriteString("⚠️ 当前未连接 UPS 设备或设备离线\n")
		notify.WechatPush(builder.String())
		return
	}

	statusStr := "已停止 ❌"
	if cfgData.Status {
		statusStr = "运行中 ✅"
	}
	builder.WriteString(fmt.Sprintf("服务状态: %s\n", statusStr))

	cap := cfgData.UpsInfo.BatteryCapacity
	if cap == "" {
		cap = "未知"
	} else {
		cap += "%"
	}
	builder.WriteString(fmt.Sprintf("当前电量: %s\n", cap))

	est := cfgData.UpsInfo.EstimateSupplyTime
	if est < 0 {
		builder.WriteString("供电状态: 市电供电中 ⚡\n")
	} else if est == 0 {
		builder.WriteString("预计续航: 正在计算中...\n")
	} else {
		builder.WriteString(fmt.Sprintf("预计续航: %d秒 (约%.1f分钟)\n", est, float64(est)/60))
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
	builder.WriteString(fmt.Sprintf("保护模式: %s\n", protectType))

	if cfgData.StandbyTime > 0 {
		unit := "秒"
		if cfgData.StandbyTimeUnit == 1 {
			unit = "分钟"
		}
		builder.WriteString(fmt.Sprintf("等待时间: %d %s后执行保护\n", cfgData.StandbyTime, unit))
	}

	notify.WechatPush(strings.TrimSpace(builder.String()))
}

func PushUGreenNotifyStatus() {
	if len(config.Config.UGreen) == 0 {
		return
	}
	cfg := config.Config.UGreen[0]
	ip, port := utils.SplitIpPort(cfg.IpPort, 9999)
	authInfo := ensureAuth(cfg.Username, cfg.Password, ip, port, cfg.UseSSL)
	if authInfo == nil {
		return
	}

	notices, _, _ := fetchUGreenNotices(authInfo, ip, port, cfg.UseSSL)
	if len(notices) > 0 {
		notify.WechatPush(buildUGreenPushContent(notices, cfg.NotifyTypeName+" 近期通知"))
	} else {
		notify.WechatPush("当前没有新的系统通知。")
	}
}

func PushUGreenDockerStatus() {
	if len(config.Config.UGreen) == 0 {
		return
	}
	cfg := config.Config.UGreen[0]
	ip, port := utils.SplitIpPort(cfg.IpPort, 9999)
	authInfo := ensureAuth(cfg.Username, cfg.Password, ip, port, cfg.UseSSL)
	if authInfo == nil {
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

	notify.WechatPush(strings.TrimSpace(builder.String()))
}

func PushUGreenPsStatus() {
	if len(config.Config.UGreen) == 0 {
		return
	}
	cfg := config.Config.UGreen[0]
	ip, port := utils.SplitIpPort(cfg.IpPort, 9999)
	authInfo := ensureAuth(cfg.Username, cfg.Password, ip, port, cfg.UseSSL)
	if authInfo == nil {
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
	builder.WriteString("📈 系统进程占用 (TOP 5)\n")
	builder.WriteString(strings.Repeat("-", 22) + "\n")
	for i, p := range allProcs {
		if i >= 5 {
			break
		}
		builder.WriteString(fmt.Sprintf("%d. %s\n   CPU: %.1f%% | 内存: %.1f%%\n", i+1, p.Name, p.Consume.CPU, p.Consume.Memory))
	}

	notify.WechatPush(strings.TrimSpace(builder.String()))
}

func PushUGreenBackupStatus() {
	if len(config.Config.UGreen) == 0 {
		return
	}
	cfg := config.Config.UGreen[0]
	ip, port := utils.SplitIpPort(cfg.IpPort, 9999)
	authInfo := ensureAuth(cfg.Username, cfg.Password, ip, port, cfg.UseSSL)
	if authInfo == nil {
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
	notify.WechatPush(strings.TrimSpace(builder.String()))
}

func PushUGreenPowerStatus() {
	if len(config.Config.UGreen) == 0 {
		return
	}
	cfg := config.Config.UGreen[0]
	ip, port := utils.SplitIpPort(cfg.IpPort, 9999)
	authInfo := ensureAuth(cfg.Username, cfg.Password, ip, port, cfg.UseSSL)
	if authInfo == nil {
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

	builder.WriteString(fmt.Sprintf("来电自启: %s\n", statusMap(cfgData.PowerBoot)))
	builder.WriteString(fmt.Sprintf("网络唤醒(WOL): %s\n", statusMap(cfgData.WakeOn)))
	builder.WriteString(fmt.Sprintf("内置硬盘休眠: %s\n", statusMap(cfgData.HardDriveFlag)))
	if cfgData.HardDriveFlag {
		builder.WriteString(fmt.Sprintf("休眠等待时间: %d %s\n", cfgData.HardDriveTime, unitMap(cfgData.HardDriveUnit)))
	}

	notify.WechatPush(strings.TrimSpace(builder.String()))
}

func HandleUGreenPerfCommand(command string) {
	if len(config.Config.UGreen) == 0 {
		return
	}
	cfg := config.Config.UGreen[0]
	ip, port := utils.SplitIpPort(cfg.IpPort, 9999)
	authInfo := ensureAuth(cfg.Username, cfg.Password, ip, port, cfg.UseSSL)
	if authInfo == nil {
		notify.WechatPush("❌ 控制指令失败：登录凭证无效")
		return
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
			notify.WechatPush("⚠️ 风扇指令格式错误。\n1: 静音 | 2: 正常 | 3: 全速\n例如: 风扇 2")
			return
		}
		_, err = requestUGreenDeepAPI(authInfo, ip, port, cfg.UseSSL, "GET", "/ugreen/v1/hardware/fan/start", map[string]string{"mode": strconv.Itoa(mode)}, nil)
		modes := map[int]string{1: "静音", 2: "正常", 3: "全速"}
		successMsg = fmt.Sprintf("🌀 风扇模式已成功切换为: %s", modes[mode])
	} else if isCPU {
		modeStr := strings.TrimSpace(strings.TrimPrefix(upperCommand, "CPU"))
		mode, _ := strconv.Atoi(modeStr)
		if mode < 0 || mode > 2 {
			notify.WechatPush("⚠️ CPU指令格式错误。\n0: 高性能 | 1: 均衡 | 2: 节能\n例如: CPU 1")
			return
		}
		_, err = requestUGreenDeepAPI(authInfo, ip, port, cfg.UseSSL, "POST", "/ugreen/v1/hardware/cpu/frequency", nil, map[string]interface{}{"frequency": mode})
		modes := map[int]string{0: "高性能", 1: "均衡", 2: "节能"}
		successMsg = fmt.Sprintf("⚡ CPU 模式已成功切换为: %s", modes[mode])
	}

	if err != nil {
		notify.WechatPush(fmt.Sprintf("❌ 指令执行失败: %v", err))
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
	rand.Read(b)
	aesKey := hex.EncodeToString(b)

	urlStr := fmt.Sprintf("%s://%s:%d%s", protocol, ip, port, apiPath)

	if len(params) > 0 {
		q := url.Values{}
		for k, v := range params {
			q.Set(k, v)
		}
		encQuery, _ := crypto.AESGCMEncrypt(aesKey, q.Encode())
		urlStr += "?encrypt_query=" + url.QueryEscape(encQuery)
	}

	var bodyReader io.Reader
	if body != nil {
		bodyJSON, _ := json.Marshal(body)
		encBody, _ := crypto.AESGCMEncrypt(aesKey, string(bodyJSON))
		encReq := map[string]string{"encrypt_req_body": encBody}
		encReqJSON, _ := json.Marshal(encReq)
		bodyReader = bytes.NewReader(encReqJSON)
	}

	req, _ := http.NewRequest(method, urlStr, bodyReader)

	securityCode, _ := crypto.RsaEncrypt(authInfo.PublicKey, aesKey)
	ugreenToken, _ := crypto.RsaEncrypt(authInfo.PublicKey, authInfo.Token)

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

	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
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
	newAuth, _ := loginUGreen(username, password, ip, port, useSSL)
	return newAuth
}

func loginUGreen(username, password, ip string, port int, useSSL bool) (*UGreenAuthInfo, error) {
	protocol := "http"
	if useSSL {
		protocol = "https"
	}

	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar:       jar,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}

	encPassword := password
	if username != "admin" {
		checkURL := fmt.Sprintf("%s://%s:%d/ugreen/v1/verify/check", protocol, ip, port)
		checkReqBody, _ := json.Marshal(map[string]string{"username": username})
		req, _ := http.NewRequest("POST", checkURL, bytes.NewBuffer(checkReqBody))
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
	loginReqBody, _ := json.Marshal(loginPayload)
	req2, _ := http.NewRequest("POST", loginURL, bytes.NewBuffer(loginReqBody))
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

	body2, _ := io.ReadAll(resp2.Body)
	var loginResp UGreenLoginResp
	if err := json.Unmarshal(body2, &loginResp); err != nil {
		return nil, err
	}
	if loginResp.Code != 200 {
		return nil, fmt.Errorf("login failed: code %d", loginResp.Code)
	}

	pubKeyBytes, _ := base64.StdEncoding.DecodeString(loginResp.Data.PublicKey)

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
	saveUGreenAuthInfo(ip, port, authInfo)
	return authInfo, nil
}

func fetchUGreenNotices(authInfo *UGreenAuthInfo, ip string, port int, useSSL bool) ([]UGreenNotice, int, error) {
	protocol := "http"
	if useSSL {
		protocol = "https"
	}
	urlStr := fmt.Sprintf("%s://%s:%d/ugreen/v1/desktop/message/list", protocol, ip, port)

	payload := map[string]interface{}{"level": []string{"info", "important", "warning"}, "page": 1, "size": 10}
	reqBody, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", urlStr, bytes.NewBuffer(reqBody))

	encToken, _ := crypto.RsaEncrypt(authInfo.PublicKey, authInfo.Token)

	req.Header.Set("x-specify-language", "zh-CN")
	req.Header.Set("x-ugreen-security-key", authInfo.TokenID)
	req.Header.Set("x-ugreen-token", encToken)
	if authInfo.CookieStr != "" {
		req.Header.Set("Cookie", authInfo.CookieStr)
	}

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
		encToken, _ := crypto.RsaEncrypt(authInfo.PublicKey, authInfo.Token)

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
