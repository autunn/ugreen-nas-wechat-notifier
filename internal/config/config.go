package config

import (
	"encoding/json"
	"log"
	"os"
	"sync"
)

// 全局配置与读写锁
var (
	Config AppConfig
	CfgMu  sync.RWMutex
)

// AppConfig 根配置结构
type AppConfig struct {
	AdminPassword   string  `json:"admin_password"`
	IntervalMinutes float64 `json:"interval_minutes"`

	// 企业微信参数 (完全对齐 NasWebhook)
	CorpID         string `json:"corpid"`
	AgentID        string `json:"agentid"`
	CorpSecret     string `json:"corpsecret"`
	Token          string `json:"token"`
	EncodingAESKey string `json:"encoding_aes_key"`
	ProxyURL       string `json:"proxy_url"`
	NasURL         string `json:"nas_url"`
	PhotoURL       string `json:"photo_url"`

	ZSpace []ZSpaceConfig `json:"zspace"`
	UGreen []UGreenConfig `json:"ugreen"`
	FnOs   []FnOsConfig   `json:"fnos"`
}

type ZSpaceConfig struct {
	IpPort         string `json:"ip_port"`
	Cookie         string `json:"cookie"`
	NotifyTypeName string `json:"notify_type_name"`
	UseSSL         bool   `json:"use_ssl"`
	MacAddress     string `json:"mac_address"`
}

type UGreenConfig struct {
	IpPort         string `json:"ip_port"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	NotifyTypeName string `json:"notify_type_name"`
	UseSSL         bool   `json:"use_ssl"`
	MacAddress     string `json:"mac_address"`
}

type FnOsConfig struct {
	Server         string `json:"server"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	NotifyTypeName string `json:"notify_type_name"`
	UseSSL         bool   `json:"use_ssl"`
	Cookie         string `json:"cookie"`
	MacAddress     string `json:"mac_address"`
}

// InitConfig 从当前目录加载 config/config.json
func InitConfig() {
	CfgMu.Lock()
	defer CfgMu.Unlock()

	data, err := os.ReadFile("config/config.json")
	if err != nil {
		log.Println("未找到 config/config.json，将以空配置启动，请通过网页进行配置。")
		Config = AppConfig{
			AdminPassword:   "admin",
			IntervalMinutes: 5,
		}
		return
	}

	err = json.Unmarshal(data, &Config)
	if err != nil {
		log.Fatalf("解析 config/config.json 失败: %v", err)
	}

	if Config.AdminPassword == "" {
		Config.AdminPassword = "admin"
	}
	if Config.IntervalMinutes <= 0 {
		Config.IntervalMinutes = 5
	}
}

// SaveConfig 保存配置到文件
func SaveConfig(newConfig AppConfig) error {
	CfgMu.Lock()
	defer CfgMu.Unlock()

	Config = newConfig
	data, err := json.MarshalIndent(Config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile("config/config.json", data, 0644)
}
