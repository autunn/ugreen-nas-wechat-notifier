package config

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	DefaultIntervalMinutes             = 5
	DefaultSystemStatusIntervalMinutes = 60
	DefaultWechatGatewayURL            = "http://127.0.0.1:5091"
)

var (
	Config AppConfig
	CfgMu  sync.RWMutex
)

type AppConfig struct {
	AdminPasswordHash string `json:"admin_password_hash,omitempty"`
	AdminPassword     string `json:"admin_password,omitempty"`

	IntervalMinutes             float64 `json:"interval_minutes"`
	SystemStatusIntervalMinutes float64 `json:"system_status_interval_minutes"`

	WechatGatewayURL    string `json:"wechat_gateway_url"`
	WechatGatewaySecret string `json:"wechat_gateway_secret"`
	WechatBindingCode   string `json:"wechat_binding_code"`
	WechatBound         bool   `json:"wechat_bound"`
	WechatBoundAt       string `json:"wechat_bound_at"`

	LocalNasName     string `json:"local_nas_name"`
	LocalNasPort     int    `json:"local_nas_port"`
	LocalNasUsername string `json:"local_nas_username"`
	LocalNasPassword string `json:"local_nas_password"`
}

type UGreenConfig struct {
	ID             string
	IpPort         string
	Username       string
	Password       string
	NotifyTypeName string
	UseSSL         bool
}

func appDataDir() string {
	if dir := strings.TrimSpace(os.Getenv("UGAPP_DATA_DIR")); dir != "" {
		return dir
	}
	return "data"
}

func AppDataDir() string {
	return appDataDir()
}

func configPath() string {
	return filepath.Join(appDataDir(), "config", "config.json")
}

func InitConfig() {
	CfgMu.Lock()
	defer CfgMu.Unlock()

	path := configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		log.Fatalf("create config dir failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		log.Println("config not found, entering first-time setup mode")
		Config = AppConfig{
			IntervalMinutes:             DefaultIntervalMinutes,
			SystemStatusIntervalMinutes: DefaultSystemStatusIntervalMinutes,
			WechatGatewayURL:            DefaultWechatGatewayURL,
			LocalNasPort:                9999,
		}
		return
	}

	if err := json.Unmarshal(data, &Config); err != nil {
		log.Fatalf("parse config failed: %v", err)
	}

	normalizeConfigLocked()
}

func IsInitialized() bool {
	CfgMu.RLock()
	defer CfgMu.RUnlock()
	return Config.AdminPasswordHash != ""
}

func GetAdminPasswordHash() string {
	CfgMu.RLock()
	defer CfgMu.RUnlock()
	return Config.AdminPasswordHash
}

func GetConfigSnapshot() AppConfig {
	CfgMu.RLock()
	defer CfgMu.RUnlock()
	return Config
}

func SanitizedConfigForWeb() AppConfig {
	snapshot := GetConfigSnapshot()
	snapshot.AdminPasswordHash = ""
	snapshot.AdminPassword = ""
	snapshot.WechatGatewaySecret = ""
	snapshot.LocalNasPassword = ""
	return snapshot
}

func MergeWithExistingSensitiveFields(existing, incoming AppConfig) AppConfig {
	incoming.WechatBindingCode = strings.TrimSpace(incoming.WechatBindingCode)
	if incoming.WechatGatewaySecret == "" {
		incoming.WechatGatewaySecret = existing.WechatGatewaySecret
	}
	if incoming.WechatBindingCode == "" {
		incoming.WechatBindingCode = existing.WechatBindingCode
	}
	if existing.WechatBound {
		incoming.WechatBound = true
	}
	if incoming.WechatBoundAt == "" {
		incoming.WechatBoundAt = existing.WechatBoundAt
	}
	if incoming.LocalNasPassword == "" {
		incoming.LocalNasPassword = existing.LocalNasPassword
	}
	return incoming
}

func SaveConfig(newConfig AppConfig) error {
	CfgMu.Lock()
	defer CfgMu.Unlock()

	Config = newConfig
	normalizeConfigLocked()
	Config.AdminPassword = ""

	data, err := json.MarshalIndent(Config, "", "  ")
	if err != nil {
		return err
	}

	path := configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return err
	}

	if err := os.Chmod(tmpPath, 0600); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	return nil
}

func normalizeConfigLocked() {
	if Config.IntervalMinutes <= 0 {
		Config.IntervalMinutes = DefaultIntervalMinutes
	}
	if Config.SystemStatusIntervalMinutes <= 0 {
		Config.SystemStatusIntervalMinutes = DefaultSystemStatusIntervalMinutes
	}
	if Config.LocalNasPort <= 0 {
		Config.LocalNasPort = 9999
	}
	if strings.TrimSpace(Config.LocalNasName) == "" {
		Config.LocalNasName = "本机绿联 NAS"
	}
	if strings.TrimSpace(Config.WechatGatewayURL) == "" {
		Config.WechatGatewayURL = DefaultWechatGatewayURL
	}
}
