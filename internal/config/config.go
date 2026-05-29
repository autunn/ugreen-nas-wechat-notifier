package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	ConfigPath                         = "config/config.json"
	DefaultIntervalMinutes             = 5
	DefaultSystemStatusIntervalMinutes = 60
)

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

	IntervalMinutes             float64 `json:"interval_minutes"`
	SystemStatusIntervalMinutes float64 `json:"system_status_interval_minutes"`

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

	incoming.ZSpace = mergeZSpaceSensitive(existing.ZSpace, incoming.ZSpace)
	incoming.UGreen = mergeUGreenSensitive(existing.UGreen, incoming.UGreen)
	incoming.FnOs = mergeFnOsSensitive(existing.FnOs, incoming.FnOs)

	return incoming
}

type ZSpaceConfig struct {
	ID             string `json:"id,omitempty"`
	IpPort         string `json:"ip_port"`
	Cookie         string `json:"cookie"`
	NotifyTypeName string `json:"notify_type_name"`
	UseSSL         bool   `json:"use_ssl"`
	MacAddress     string `json:"mac_address"`
}

type UGreenConfig struct {
	ID             string `json:"id,omitempty"`
	IpPort         string `json:"ip_port"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	NotifyTypeName string `json:"notify_type_name"`
	UseSSL         bool   `json:"use_ssl"`
	MacAddress     string `json:"mac_address"`
}

type FnOsConfig struct {
	ID             string `json:"id,omitempty"`
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
			IntervalMinutes:             DefaultIntervalMinutes,
			SystemStatusIntervalMinutes: DefaultSystemStatusIntervalMinutes,
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
		Config.IntervalMinutes = DefaultIntervalMinutes
	}
	if Config.SystemStatusIntervalMinutes <= 0 {
		Config.SystemStatusIntervalMinutes = DefaultSystemStatusIntervalMinutes
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

	usedIDs := map[string]struct{}{}
	Config.ZSpace = ensureZSpaceIDs(Config.ZSpace, usedIDs)
	Config.UGreen = ensureUGreenIDs(Config.UGreen, usedIDs)
	Config.FnOs = ensureFnOsIDs(Config.FnOs, usedIDs)
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

func mergeZSpaceSensitive(existing, incoming []ZSpaceConfig) []ZSpaceConfig {
	if len(existing) == 0 || len(incoming) == 0 {
		return incoming
	}

	if len(existing) == 1 && len(incoming) == 1 && strings.TrimSpace(incoming[0].ID) == "" {
		if incoming[0].Cookie == "" {
			incoming[0].Cookie = existing[0].Cookie
		}
		return incoming
	}

	existingByID := make(map[string]ZSpaceConfig, len(existing))
	for _, item := range existing {
		if id := strings.TrimSpace(item.ID); id != "" {
			existingByID[id] = item
		}
	}

	for i := range incoming {
		id := strings.TrimSpace(incoming[i].ID)
		if id == "" {
			continue
		}
		if old, ok := existingByID[id]; ok && incoming[i].Cookie == "" {
			incoming[i].Cookie = old.Cookie
		}
	}
	return incoming
}

func mergeUGreenSensitive(existing, incoming []UGreenConfig) []UGreenConfig {
	if len(existing) == 0 || len(incoming) == 0 {
		return incoming
	}

	if len(existing) == 1 && len(incoming) == 1 && strings.TrimSpace(incoming[0].ID) == "" {
		if incoming[0].Password == "" {
			incoming[0].Password = existing[0].Password
		}
		return incoming
	}

	existingByID := make(map[string]UGreenConfig, len(existing))
	for _, item := range existing {
		if id := strings.TrimSpace(item.ID); id != "" {
			existingByID[id] = item
		}
	}

	for i := range incoming {
		id := strings.TrimSpace(incoming[i].ID)
		if id == "" {
			continue
		}
		if old, ok := existingByID[id]; ok && incoming[i].Password == "" {
			incoming[i].Password = old.Password
		}
	}
	return incoming
}

func mergeFnOsSensitive(existing, incoming []FnOsConfig) []FnOsConfig {
	if len(existing) == 0 || len(incoming) == 0 {
		return incoming
	}

	if len(existing) == 1 && len(incoming) == 1 && strings.TrimSpace(incoming[0].ID) == "" {
		if incoming[0].Password == "" {
			incoming[0].Password = existing[0].Password
		}
		if incoming[0].Cookie == "" {
			incoming[0].Cookie = existing[0].Cookie
		}
		return incoming
	}

	existingByID := make(map[string]FnOsConfig, len(existing))
	for _, item := range existing {
		if id := strings.TrimSpace(item.ID); id != "" {
			existingByID[id] = item
		}
	}

	for i := range incoming {
		id := strings.TrimSpace(incoming[i].ID)
		if id == "" {
			continue
		}
		if old, ok := existingByID[id]; ok {
			if incoming[i].Password == "" {
				incoming[i].Password = old.Password
			}
			if incoming[i].Cookie == "" {
				incoming[i].Cookie = old.Cookie
			}
		}
	}
	return incoming
}

func ensureZSpaceIDs(items []ZSpaceConfig, used map[string]struct{}) []ZSpaceConfig {
	for i := range items {
		items[i].ID = ensureConfigID(items[i].ID, used)
	}
	return items
}

func ensureUGreenIDs(items []UGreenConfig, used map[string]struct{}) []UGreenConfig {
	for i := range items {
		items[i].ID = ensureConfigID(items[i].ID, used)
	}
	return items
}

func ensureFnOsIDs(items []FnOsConfig, used map[string]struct{}) []FnOsConfig {
	for i := range items {
		items[i].ID = ensureConfigID(items[i].ID, used)
	}
	return items
}

func ensureConfigID(current string, used map[string]struct{}) string {
	id := strings.TrimSpace(current)
	if id != "" {
		if _, exists := used[id]; !exists {
			used[id] = struct{}{}
			return id
		}
	}

	for {
		generated := randomConfigID()
		if _, exists := used[generated]; exists {
			continue
		}
		used[generated] = struct{}{}
		return generated
	}
}

func randomConfigID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
