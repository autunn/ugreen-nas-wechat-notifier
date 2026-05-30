package main

import (
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"nasnotify-go/internal/config"
	"nasnotify-go/internal/notify"
)

var setupToken string

type setupRequest struct {
	InitToken     string           `json:"init_token"`
	AdminPassword string           `json:"admin_password"`
	Config        config.AppConfig `json:"config"`
}

type saveRequest struct {
	NewAdminPassword string           `json:"new_admin_password"`
	Config           config.AppConfig `json:"config"`
}

type loginRequest struct {
	Password string `json:"password"`
}

type verifyCodeRequest struct {
	VerifyCode string `json:"verify_code"`
}

type bootstrapResponse struct {
	Initialized   bool             `json:"initialized"`
	Authenticated bool             `json:"authenticated"`
	Version       string           `json:"version"`
	Config        config.AppConfig `json:"config"`
	SetupToken    string           `json:"setup_token,omitempty"`
}

func ensureSetupToken() {
	if setupToken != "" {
		return
	}

	setupToken = randomHex(16)
	log.Println("============================================================")
	log.Println("System is not initialized yet. Complete setup from the packaged frontend.")
	log.Printf("Setup token: %s\n", setupToken)
	log.Println("============================================================")
}

func buildBootstrapResponse(c *gin.Context) bootstrapResponse {
	resp := bootstrapResponse{
		Initialized:   config.IsInitialized(),
		Authenticated: checkCookie(c),
		Version:       Version,
		Config:        config.SanitizedConfigForWeb(),
	}
	if !resp.Initialized {
		ensureSetupToken()
		resp.SetupToken = setupToken
	}
	return resp
}

func authenticateAdminPassword(password string) bool {
	hash := config.GetAdminPasswordHash()
	if hash == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func performInitialSetup(req setupRequest) (int, string) {
	if config.IsInitialized() {
		return http.StatusForbidden, "system already initialized"
	}

	ensureSetupToken()
	if !secureCompare(req.InitToken, setupToken) {
		return http.StatusForbidden, "invalid setup token"
	}

	password := strings.TrimSpace(req.AdminPassword)
	if len(password) < 8 {
		return http.StatusBadRequest, "admin password must be at least 8 characters"
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return http.StatusInternalServerError, "password hash failed"
	}

	newConfig := req.Config
	newConfig.AdminPasswordHash = string(hash)
	newConfig.AdminPassword = ""

	if err := config.SaveConfig(newConfig); err != nil {
		log.Printf("save initial config failed: %v", err)
		return http.StatusInternalServerError, "save config failed"
	}
	if _, err := notify.EnsureClawBotBindingCode(); err != nil {
		log.Printf("ensure wechat binding code failed: %v", err)
	}

	setupToken = ""
	return http.StatusOK, ""
}

func saveAppConfig(req saveRequest) (int, string) {
	oldConfig := config.GetConfigSnapshot()
	newConfig := config.MergeWithExistingSensitiveFields(oldConfig, req.Config)
	newConfig.AdminPasswordHash = oldConfig.AdminPasswordHash
	newConfig.AdminPassword = ""

	oldGatewayURL := strings.TrimSpace(oldConfig.WechatGatewayURL)
	oldGatewaySecret := strings.TrimSpace(oldConfig.WechatGatewaySecret)
	newGatewayURL := strings.TrimSpace(newConfig.WechatGatewayURL)
	newGatewaySecret := strings.TrimSpace(newConfig.WechatGatewaySecret)
	if oldGatewayURL != newGatewayURL || oldGatewaySecret != newGatewaySecret {
		newConfig.WechatBound = false
		newConfig.WechatBoundAt = ""
		newConfig.WechatBindingCode = ""
		notify.InvalidateClawBotAccessKey()
	}

	newPassword := strings.TrimSpace(req.NewAdminPassword)
	if newPassword != "" {
		if len(newPassword) < 8 {
			return http.StatusBadRequest, "admin password must be at least 8 characters"
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
		if err != nil {
			return http.StatusInternalServerError, "password hash failed"
		}
		newConfig.AdminPasswordHash = string(hash)
	}

	if err := config.SaveConfig(newConfig); err != nil {
		log.Printf("save config failed: %v", err)
		return http.StatusInternalServerError, "save failed"
	}
	if _, err := notify.EnsureClawBotBindingCode(); err != nil {
		log.Printf("ensure wechat binding code failed: %v", err)
	}

	return http.StatusOK, ""
}

func migrateLegacyAdminPassword() {
	cfg := config.GetConfigSnapshot()

	if cfg.AdminPasswordHash != "" {
		if cfg.AdminPassword != "" {
			cfg.AdminPassword = ""
			if err := config.SaveConfig(cfg); err != nil {
				log.Printf("clear legacy plain password field failed: %v", err)
			}
		}
		return
	}

	legacyPassword := strings.TrimSpace(cfg.AdminPassword)
	if legacyPassword == "" {
		return
	}

	cfg.AdminPassword = ""
	if legacyPassword == "admin" {
		log.Println("detected legacy default admin password, clearing it and entering setup mode")
		if err := config.SaveConfig(cfg); err != nil {
			log.Printf("clear legacy default password failed: %v", err)
		}
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(legacyPassword), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("migrate legacy password failed: %v", err)
		return
	}
	cfg.AdminPasswordHash = string(hash)

	if err := config.SaveConfig(cfg); err != nil {
		log.Printf("save migrated password hash failed: %v", err)
		return
	}

	log.Println("migrated legacy plain password to bcrypt hash")
}
