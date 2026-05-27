package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"nasnotify-go/internal/api"
	"nasnotify-go/internal/config"
	"nasnotify-go/internal/nas"
	"nasnotify-go/internal/notify"
)

var sessionToken string
var setupToken string

var Version = "v2026.05.01"

type setupRequest struct {
	InitToken     string           `json:"init_token"`
	AdminPassword string           `json:"admin_password"`
	Config        config.AppConfig `json:"config"`
}

type saveRequest struct {
	NewAdminPassword string           `json:"new_admin_password"`
	Config           config.AppConfig `json:"config"`
}

func init() {
	sessionToken = randomHex(32)
}

func main() {
	config.InitConfig()
	migrateLegacyAdminPassword()

	if err := os.MkdirAll("config", 0755); err != nil {
		log.Fatalf("创建 config 目录失败: %v", err)
	}
	if err := os.MkdirAll("data/log", 0755); err != nil {
		log.Fatalf("创建 data/log 目录失败: %v", err)
	}
	if err := os.MkdirAll("data/token", 0755); err != nil {
		log.Fatalf("创建 data/token 目录失败: %v", err)
	}

	if !config.IsInitialized() {
		ensureSetupToken()
	}

	go runTasksLoop()
	go runHourlyTasksLoop()

	if config.IsInitialized() {
		go func() {
			time.Sleep(5 * time.Second)
			notify.CreateWechatMenu()
		}()
	}

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	r.LoadHTMLGlob("templates/*")

	// 企业微信回调仍然公开
	r.GET("/wx-receive", api.HandleVerify)
	r.POST("/wx-receive", api.HandleMessage)

	// 首次初始化页面
	r.GET("/setup", func(c *gin.Context) {
		if config.IsInitialized() {
			c.Redirect(http.StatusFound, "/login")
			return
		}

		ensureSetupToken()

		prefillToken := ""
		if secureCompare(c.Query("token"), setupToken) {
			prefillToken = c.Query("token")
		}

		c.HTML(http.StatusOK, "setup.html", gin.H{
			"version":    Version,
			"setupToken": prefillToken,
		})
	})

	r.POST("/setup", func(c *gin.Context) {
		if config.IsInitialized() {
			c.JSON(http.StatusForbidden, gin.H{"error": "系统已初始化，不能重复初始化"})
			return
		}

		ensureSetupToken()

		var req setupRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "JSON 解析失败"})
			return
		}

		if !secureCompare(req.InitToken, setupToken) {
			c.JSON(http.StatusForbidden, gin.H{"error": "初始化令牌错误，请查看启动日志"})
			return
		}

		password := strings.TrimSpace(req.AdminPassword)
		if len(password) < 8 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "管理员密码至少 8 位"})
			return
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "密码加密失败"})
			return
		}

		newConfig := req.Config
		newConfig.AdminPasswordHash = string(hash)
		newConfig.AdminPassword = ""

		if err := config.SaveConfig(newConfig); err != nil {
			log.Printf("首次初始化保存配置失败: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "保存配置失败"})
			return
		}

		setupToken = ""
		setAuthCookie(c)

		go notify.CreateWechatMenu()

		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	r.GET("/login", func(c *gin.Context) {
		if !config.IsInitialized() {
			c.Redirect(http.StatusFound, "/setup")
			return
		}

		if checkCookie(c) {
			c.Redirect(http.StatusFound, "/")
			return
		}

		c.HTML(http.StatusOK, "login.html", gin.H{"version": Version})
	})

	r.POST("/login", func(c *gin.Context) {
		if !config.IsInitialized() {
			c.Redirect(http.StatusFound, "/setup")
			return
		}

		password := c.PostForm("password")
		hash := config.GetAdminPasswordHash()

		if hash != "" && bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil {
			setAuthCookie(c)
			c.Redirect(http.StatusFound, "/")
			return
		}

		c.HTML(http.StatusUnauthorized, "login.html", gin.H{
			"error":   "密码错误",
			"version": Version,
		})
	})

	r.GET("/logout", func(c *gin.Context) {
		clearAuthCookie(c)
		c.Redirect(http.StatusFound, "/login")
	})

	auth := r.Group("/")
	auth.Use(authMiddleware())
	{
		auth.GET("/", func(c *gin.Context) {
			webConfig := config.SanitizedConfigForWeb()
			configJsonBytes, _ := json.Marshal(webConfig)

			c.HTML(http.StatusOK, "index.html", gin.H{
				"configJson": template.JS(configJsonBytes),
				"success":    c.Query("success") == "true",
				"version":    Version,
			})
		})

		auth.GET("/test-push", func(c *gin.Context) {
			go notify.WechatPush(" 测试通知\n\n这是一条来自 NasNotify 的测试消息！如果您收到此消息，说明企业微信推送配置已完全正确。")
			c.JSON(http.StatusOK, gin.H{"success": true, "msg": "测试请求已触发"})
		})

		auth.POST("/save", func(c *gin.Context) {
			var req saveRequest
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "JSON 解析失败"})
				return
			}

			oldConfig := config.GetConfigSnapshot()
			newConfig := req.Config

			// 保存配置时，默认保留旧密码 hash
			newConfig.AdminPasswordHash = oldConfig.AdminPasswordHash
			newConfig.AdminPassword = ""

			newPassword := strings.TrimSpace(req.NewAdminPassword)
			if newPassword != "" {
				if len(newPassword) < 8 {
					c.JSON(http.StatusBadRequest, gin.H{"error": "新管理员密码至少 8 位"})
					return
				}

				hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "密码加密失败"})
					return
				}

				newConfig.AdminPasswordHash = string(hash)
			}

			if err := config.SaveConfig(newConfig); err != nil {
				log.Printf("保存配置失败: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "保存失败"})
				return
			}

			go notify.CreateWechatMenu()

			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})
	}

	log.Printf("聚合通知中心 %s 已启动！", Version)
	if err := r.Run(":5080"); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}

func authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !config.IsInitialized() {
			c.Redirect(http.StatusFound, "/setup")
			c.Abort()
			return
		}

		if !checkCookie(c) {
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}

		c.Next()
	}
}

func checkCookie(c *gin.Context) bool {
	cookie, err := c.Cookie("auth_session")
	if err != nil {
		return false
	}

	return secureCompare(cookie, sessionToken)
}

func setAuthCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("auth_session", sessionToken, 86400, "/", "", isHTTPS(c), true)
}

func clearAuthCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("auth_session", "", -1, "/", "", isHTTPS(c), true)
}

func isHTTPS(c *gin.Context) bool {
	if c.Request.TLS != nil {
		return true
	}

	proto := strings.ToLower(c.GetHeader("X-Forwarded-Proto"))
	return proto == "https"
}

func secureCompare(a, b string) bool {
	if a == "" || b == "" {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func randomHex(byteLen int) string {
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func ensureSetupToken() {
	if setupToken != "" {
		return
	}

	setupToken = randomHex(32)
	log.Println("============================================================")
	log.Println("系统尚未初始化，请使用以下地址完成首次配置：")
	log.Printf("http://<你的IP或域名>:5080/setup?token=%s\n", setupToken)
	log.Println("如果直接访问 http://<你的IP或域名>:5080/setup，也可以手动输入上述初始化令牌。")
	log.Println("============================================================")
}

// migrateLegacyAdminPassword 迁移旧版明文管理员密码。
// - 如果旧密码是 admin：清除它，强制进入首次初始化。
// - 如果旧密码不是 admin：迁移成 bcrypt hash。
// - 迁移后不会再保存 admin_password 明文字段。
func migrateLegacyAdminPassword() {
	cfg := config.GetConfigSnapshot()

	if cfg.AdminPasswordHash != "" {
		if cfg.AdminPassword != "" {
			cfg.AdminPassword = ""
			if err := config.SaveConfig(cfg); err != nil {
				log.Printf("清理旧版明文密码字段失败: %v", err)
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
		log.Println("检测到旧版默认管理员密码 admin，已清除，将进入首次初始化模式。")
		if err := config.SaveConfig(cfg); err != nil {
			log.Printf("清除旧版默认密码失败: %v", err)
		}
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(legacyPassword), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("迁移旧版明文密码失败: %v", err)
		return
	}

	cfg.AdminPasswordHash = string(hash)

	if err := config.SaveConfig(cfg); err != nil {
		log.Printf("保存迁移后的密码 hash 失败: %v", err)
		return
	}

	log.Println("已将旧版明文管理员密码迁移为 bcrypt hash。")
}

func runTasksLoop() {
	time.Sleep(2 * time.Second)

	for {
		config.CfgMu.RLock()
		interval := config.Config.IntervalMinutes
		config.CfgMu.RUnlock()

		if interval <= 0 {
			interval = 5
		}

		log.Println("--- 开始执行后台通知抓取任务 ---")
		nas.ProcessZSpace()
		nas.ProcessUGreen()
		nas.ProcessFnOs()

		time.Sleep(time.Duration(interval * float64(time.Minute)))
	}
}

func runHourlyTasksLoop() {
	time.Sleep(3 * time.Second)

	for {
		now := time.Now()
		nextHour := now.Truncate(time.Hour).Add(time.Hour)
		waitDuration := time.Until(nextHour)

		time.Sleep(waitDuration)

		log.Println("--- ⏰ 触发整点系统状态推送任务 ---")
		nas.PushUGreenSystemStatus()
	}
}
