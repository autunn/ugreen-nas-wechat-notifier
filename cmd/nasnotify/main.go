package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"nasnotify-go/internal/api"
	"nasnotify-go/internal/config"
	"nasnotify-go/internal/nas"
	"nasnotify-go/internal/notify"
)

var sessionToken string
var Version = "v2026.05.01"

func init() {
	b := make([]byte, 16)
	rand.Read(b)
	sessionToken = hex.EncodeToString(b)
}

func main() {
	config.InitConfig()
	os.MkdirAll("config", 0755)
	os.MkdirAll("data/log", 0755)
	os.MkdirAll("data/token", 0755)

	go runTasksLoop()
	go runHourlyTasksLoop()

	go func() {
		time.Sleep(5 * time.Second)
		notify.CreateWechatMenu()
	}()

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	r.LoadHTMLGlob("templates/*")

	r.GET("/wx-receive", api.HandleVerify)
	r.POST("/wx-receive", api.HandleMessage)

	r.GET("/login", func(c *gin.Context) {
		if checkCookie(c) {
			c.Redirect(http.StatusFound, "/")
			return
		}
		c.HTML(http.StatusOK, "login.html", gin.H{"version": Version})
	})

	r.POST("/login", func(c *gin.Context) {
		password := c.PostForm("password")
		config.CfgMu.RLock()
		adminPass := config.Config.AdminPassword
		config.CfgMu.RUnlock()

		if password == adminPass {
			c.SetCookie("auth_session", sessionToken, 86400, "/", "", false, true)
			c.Redirect(http.StatusFound, "/")
		} else {
			c.HTML(http.StatusUnauthorized, "login.html", gin.H{"error": "密码错误", "version": Version})
		}
	})

	r.GET("/logout", func(c *gin.Context) {
		c.SetCookie("auth_session", "", -1, "/", "", false, true)
		c.Redirect(http.StatusFound, "/login")
	})

	auth := r.Group("/")
	auth.Use(authMiddleware())
	{
		auth.GET("/", func(c *gin.Context) {
			config.CfgMu.RLock()
			configJsonBytes, _ := json.Marshal(config.Config)
			config.CfgMu.RUnlock()

			c.HTML(http.StatusOK, "index.html", gin.H{
				"configJson": template.JS(configJsonBytes),
				"success":    c.Query("success") == "true",
				"version":    Version,
			})
		})
		auth.GET("/test-push", func(c *gin.Context) {
			go notify.WechatPush("🔔 测试通知\n\n这是一条来自 NasNotify 的测试消息！如果您收到此消息，说明企业微信推送配置已完全正确。")
			c.JSON(http.StatusOK, gin.H{"success": true, "msg": "测试请求已触发"})
		})
		auth.POST("/save", func(c *gin.Context) {
			var newConfig config.AppConfig
			if err := c.ShouldBindJSON(&newConfig); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "JSON解析失败"})
				return
			}
			if err := config.SaveConfig(newConfig); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "保存失败"})
				return
			}
			go notify.CreateWechatMenu()
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})
	}

	log.Printf("聚合通知中心 %s 已启动！", Version)
	r.Run(":5080")
}

func authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
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
	return err == nil && cookie == sessionToken
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
