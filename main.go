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
)

var sessionToken string
var Version = "v2026.05.01" // 加入版本号机制，可通过编译时 -ldflags 动态覆盖

func init() {
	b := make([]byte, 16)
	rand.Read(b)
	sessionToken = hex.EncodeToString(b)
}

func main() {
	// 1. 初始化配置和运行时目录
	initConfig()
	os.MkdirAll("config", 0755)
	os.MkdirAll("data/log", 0755)
	os.MkdirAll("data/token", 0755)

	// 2. 在后台独立协程中启动 NAS 轮询任务
	go runTasksLoop()

	// 启动整点状态报告任务
	go runHourlyTasksLoop()

	// === 新增：程序启动 5 秒后，自动尝试创建/更新一次企业微信自建应用菜单 ===
	go func() {
		time.Sleep(5 * time.Second)
		CreateWechatMenu()
	}()

	// 3. 启动 Web 路由服务
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	// 加载静态文件与模板
	r.LoadHTMLGlob("templates/*")

	// 企业微信回调与通用 Webhook 接收
	r.GET("/wx-receive", handleVerify)
	r.POST("/wx-receive", handleMessage)

	// --- 路由配置 ---

	// 登录页面
	r.GET("/login", func(c *gin.Context) {
		if checkCookie(c) {
			c.Redirect(http.StatusFound, "/")
			return
		}
		c.HTML(http.StatusOK, "login.html", gin.H{"version": Version})
	})

	// 处理登录请求
	r.POST("/login", func(c *gin.Context) {
		password := c.PostForm("password")

		CfgMu.RLock()
		adminPass := Config.AdminPassword
		CfgMu.RUnlock()

		if password == adminPass {
			// 密码正确，写入 Cookie (有效期一天)
			c.SetCookie("auth_session", sessionToken, 86400, "/", "", false, true)
			c.Redirect(http.StatusFound, "/")
		} else {
			c.HTML(http.StatusUnauthorized, "login.html", gin.H{"error": "密码错误", "version": Version})
		}
	})

	// 退出登录
	r.GET("/logout", func(c *gin.Context) {
		c.SetCookie("auth_session", "", -1, "/", "", false, true)
		c.Redirect(http.StatusFound, "/login")
	})

	// 需要鉴权的路由组 (后台管理界面)
	auth := r.Group("/")
	auth.Use(authMiddleware())
	{
		// 渲染后台主页
		auth.GET("/", func(c *gin.Context) {
			CfgMu.RLock()
			configJsonBytes, _ := json.Marshal(Config)
			CfgMu.RUnlock()

			c.HTML(http.StatusOK, "index.html", gin.H{
				"configJson": template.JS(configJsonBytes),
				"success":    c.Query("success") == "true",
				"version":    Version,
			})
		})
		// 触发测试推送的 API 接口
		auth.GET("/test-push", func(c *gin.Context) {
			go WechatPush("🔔 测试通知\n\n这是一条来自 NasNotify 的测试消息！如果您收到此消息，说明企业微信推送配置已完全正确。")
			c.JSON(http.StatusOK, gin.H{"success": true, "msg": "测试请求已触发"})
		})

		// 接收前端发来的保存配置请求
		auth.POST("/save", func(c *gin.Context) {
			var newConfig AppConfig
			if err := c.ShouldBindJSON(&newConfig); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "JSON解析失败"})
				return
			}

			if err := SaveConfig(newConfig); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "保存失败"})
				return
			}

			// === 新增：保存新配置后，异步重新同步一次企业微信菜单 ===
			go CreateWechatMenu()

			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})
	}

	log.Printf("=================================================")
	log.Printf("聚合通知中心 %s 已启动！", Version)
	log.Printf("Web 控制台地址: http://localhost:5080")
	log.Printf("企业微信 Webhook 接收地址: http://你的外网IP或域名:5080/wx-receive")
	log.Printf("=================================================")
	r.Run(":5080")
}

// authMiddleware 鉴权中间件拦截器
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

// checkCookie 校验用户身份
func checkCookie(c *gin.Context) bool {
	cookie, err := c.Cookie("auth_session")
	return err == nil && cookie == sessionToken
}

// runTasksLoop 常规轮询任务（处理系统通知与离线告警）
func runTasksLoop() {
	time.Sleep(2 * time.Second)

	for {
		CfgMu.RLock()
		interval := Config.IntervalMinutes
		CfgMu.RUnlock()

		if interval <= 0 {
			interval = 5 // 兜底间隔
		}

		log.Println("--- 开始执行后台通知抓取任务 ---")
		ProcessZSpace()
		ProcessUGreen()
		ProcessFnOs()

		time.Sleep(time.Duration(interval * float64(time.Minute)))
	}
}

// runHourlyTasksLoop 独立协程处理整点任务
func runHourlyTasksLoop() {
	time.Sleep(3 * time.Second)

	for {
		now := time.Now()
		nextHour := now.Truncate(time.Hour).Add(time.Hour)
		waitDuration := time.Until(nextHour)

		log.Printf("⏳ 状态报告任务已就绪，将在 %v 后 (即 %s) 准时执行\n", waitDuration.Round(time.Second), nextHour.Format("15:04:05"))

		time.Sleep(waitDuration)

		log.Println("--- ⏰ 触发整点系统状态推送任务 ---")
		PushUGreenSystemStatus()
	}
}
