package config

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
)

const ConfigPath = "config/config.json"

// 全局配置与读写锁
var (
	Config AppConfig
	CfgMu  sync.RWMutex
)

// AppConfig 根配置结构
type AppConfig struct {
	// 新版字段：只保存 bcrypt hash，不保存明文密码
	AdminPasswordHash string `json:"admin_password_hash,omitempty"`

	// 旧版兼容字段：只用于迁移旧配置，不再保存明文密码
	AdminPassword string `json:"admin_password,omitempty"`

	IntervalMinutes float64 `json:"interval_minutes"`

	// 企业微信参数
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

// MergeWithExistingSensitiveFields 用已有配置补齐前端未回填的敏感字段。
func MergeWithExistingSensitiveFields(existing, incoming AppConfig) AppConfig {
	if incoming.CorpSecret == "" {
		incoming.CorpSecret = existing.CorpSecret
	}
	if incoming.Token == "" {
		incoming.Token = existing.Token
	}
	if incoming.EncodingAESKey == "" {
		incoming.EncodingAESKey = existing.EncodingAESKey
	}

	if len(incoming.ZSpace) == 1 && len(existing.ZSpace) > 0 {
		if incoming.ZSpace[0].Cookie == "" {
			incoming.ZSpace[0].Cookie = existing.ZSpace[0].Cookie
		}
	}
	if len(incoming.UGreen) == 1 && len(existing.UGreen) > 0 {
		if incoming.UGreen[0].Password == "" {
			incoming.UGreen[0].Password = existing.UGreen[0].Password
		}
	}
	if len(incoming.FnOs) == 1 && len(existing.FnOs) > 0 {
		if incoming.FnOs[0].Password == "" {
			incoming.FnOs[0].Password = existing.FnOs[0].Password
		}
		if incoming.FnOs[0].Cookie == "" {
			incoming.FnOs[0].Cookie = existing.FnOs[0].Cookie
		}
	}

	return incoming
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

// InitConfig 从当前目录加载 config/config.json。
// 找不到配置文件时不再设置默认密码，进入首次初始化模式。
func InitConfig() {
	CfgMu.Lock()
	defer CfgMu.Unlock()

	if err := os.MkdirAll(filepath.Dir(ConfigPath), 0755); err != nil {
		log.Fatalf("创建配置目录失败: %v", err)
	}

	data, err := os.ReadFile(ConfigPath)
	if err != nil {
		log.Println("未找到 config/config.json，将进入首次初始化模式。")
		Config = AppConfig{
			IntervalMinutes: 5,
		}
		return
	}

	if err := json.Unmarshal(data, &Config); err != nil {
		log.Fatalf("解析 config/config.json 失败: %v", err)
	}

	normalizeConfigLocked()
}

// IsInitialized 判断系统是否已经完成初始化。
// 只认 admin_password_hash，不再认旧版明文 admin_password。
func IsInitialized() bool {
	CfgMu.RLock()
	defer CfgMu.RUnlock()
	return Config.AdminPasswordHash != ""
}

// GetAdminPasswordHash 返回管理员密码 hash。
func GetAdminPasswordHash() string {
	CfgMu.RLock()
	defer CfgMu.RUnlock()
	return Config.AdminPasswordHash
}

// GetConfigSnapshot 返回当前配置快照。
func GetConfigSnapshot() AppConfig {
	CfgMu.RLock()
	defer CfgMu.RUnlock()

	snapshot := Config
	snapshot.ZSpace = cloneZSpace(Config.ZSpace)
	snapshot.UGreen = cloneUGreen(Config.UGreen)
	snapshot.FnOs = cloneFnOs(Config.FnOs)

	return snapshot
}

// SanitizedConfigForWeb 返回给前端使用的配置。
// 注意：不返回管理员密码 hash，也不返回旧版明文密码。
func SanitizedConfigForWeb() AppConfig {
	snapshot := GetConfigSnapshot()
	snapshot.AdminPasswordHash = ""
	snapshot.AdminPassword = ""
	snapshot.CorpSecret = ""
	snapshot.Token = ""
	snapshot.EncodingAESKey = ""
	for i := range snapshot.ZSpace {
		snapshot.ZSpace[i].Cookie = ""
	}
	for i := range snapshot.UGreen {
		snapshot.UGreen[i].Password = ""
	}
	for i := range snapshot.FnOs {
		snapshot.FnOs[i].Password = ""
		snapshot.FnOs[i].Cookie = ""
	}
	return snapshot
}

// SaveConfig 保存配置到文件。
func SaveConfig(newConfig AppConfig) error {
	CfgMu.Lock()
	defer CfgMu.Unlock()

	Config = newConfig
	normalizeConfigLocked()

	// 绝不保存旧版明文后台密码
	Config.AdminPassword = ""

	data, err := json.MarshalIndent(Config, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(ConfigPath), 0755); err != nil {
		return err
	}

	tmpPath := ConfigPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return err
	}

	if err := os.Chmod(tmpPath, 0600); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	if err := os.Rename(tmpPath, ConfigPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	return nil
}

func normalizeConfigLocked() {
	if Config.IntervalMinutes <= 0 {
		Config.IntervalMinutes = 5
	}

	if Config.ZSpace == nil {
		Config.ZSpace = []ZSpaceConfig{}
	}
	if Config.UGreen == nil {
		Config.UGreen = []UGreenConfig{}
	}
	if Config.FnOs == nil {
		Config.FnOs = []FnOsConfig{}
	}
}

func cloneZSpace(in []ZSpaceConfig) []ZSpaceConfig {
	if in == nil {
		return nil
	}
	out := make([]ZSpaceConfig, len(in))
	copy(out, in)
	return out
}

func cloneUGreen(in []UGreenConfig) []UGreenConfig {
	if in == nil {
		return nil
	}
	out := make([]UGreenConfig, len(in))
	copy(out, in)
	return out
}

func cloneFnOs(in []FnOsConfig) []FnOsConfig {
	if in == nil {
		return nil
	}
	out := make([]FnOsConfig, len(in))
	copy(out, in)
	return out
}
