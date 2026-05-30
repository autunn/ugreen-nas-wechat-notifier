package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	qrcode "github.com/skip2/go-qrcode"

	"nasnotify-go/internal/config"
	"nasnotify-go/internal/notify"
)

func registerAppRoutes(r *gin.Engine, prefix string) {
	r.GET(routePath(prefix, "/healthz"), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	apiGroup := r.Group(routePath(prefix, "/api"))
	{
		apiGroup.GET("/bootstrap", func(c *gin.Context) {
			c.JSON(http.StatusOK, buildBootstrapResponse(c))
		})

		apiGroup.POST("/setup", func(c *gin.Context) {
			var req setupRequest
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
				return
			}

			status, message := performInitialSetup(req)
			if message != "" {
				c.JSON(status, gin.H{"error": message})
				return
			}

			setAuthCookie(c)
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		apiGroup.POST("/login", func(c *gin.Context) {
			if !config.IsInitialized() {
				c.JSON(http.StatusForbidden, gin.H{"error": "system not initialized"})
				return
			}

			var req loginRequest
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
				return
			}

			if authenticateAdminPassword(req.Password) {
				setAuthCookie(c)
				c.JSON(http.StatusOK, gin.H{"status": "ok"})
				return
			}

			c.JSON(http.StatusUnauthorized, gin.H{"error": "password incorrect"})
		})

		apiGroup.POST("/logout", func(c *gin.Context) {
			clearAuthCookie(c)
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})
	}

	apiAuth := apiGroup.Group("")
	apiAuth.Use(apiAuthMiddleware())
	{
		apiAuth.GET("/wechat/status", func(c *gin.Context) {
			c.JSON(http.StatusOK, notify.GetClawBotStatus())
		})
		apiAuth.GET("/wechat/qrcode", proxyWechatQRCode)

		apiAuth.POST("/wechat/unbind", func(c *gin.Context) {
			if err := notify.UnbindClawBot(); err != nil {
				c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		apiAuth.POST("/wechat/login/start", func(c *gin.Context) {
			if err := notify.StartClawBotLogin(); err != nil {
				c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		apiAuth.POST("/wechat/login/verify-code", func(c *gin.Context) {
			var req verifyCodeRequest
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
				return
			}
			if err := notify.SubmitClawBotVerifyCode(req.VerifyCode); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		apiAuth.POST("/test-push", func(c *gin.Context) {
			if err := triggerTestPush(); err != nil {
				c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"success": true, "msg": "test push sent"})
		})

		apiAuth.POST("/save", func(c *gin.Context) {
			var req saveRequest
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
				return
			}

			status, message := saveAppConfig(req)
			if message != "" {
				c.JSON(status, gin.H{"error": message})
				return
			}

			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})
	}
}

func routePath(prefix, p string) string {
	if prefix == "/" || prefix == "" {
		return p
	}
	if p == "/" {
		return prefix + "/"
	}
	return prefix + p
}

func registerFrontendRoutes(r *gin.Engine, frontendDir string) {
	indexFS := http.Dir(frontendDir)
	assetFS := http.Dir(filepath.Join(frontendDir, "src"))
	r.StaticFS("/src", assetFS)
	r.StaticFS("/nasnotify/src", assetFS)

	r.GET("/", serveFrontendIndex(indexFS))
	r.GET("/nasnotify", serveFrontendIndex(indexFS))
	r.GET("/nasnotify/", serveFrontendIndex(indexFS))
}

func findFrontendDir() string {
	candidates := []string{
		strings.TrimSpace(os.Getenv("UGAPP_WEB_DIR")),
		"www",
		filepath.Join(filepath.Dir(os.Args[0]), "..", "www"),
		filepath.Join("packaging", "ugreen-native-app", "rootfs_common", "www"),
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		indexPath := filepath.Join(candidate, "index.html")
		if info, err := os.Stat(indexPath); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

func serveFrontendIndex(fileSystem http.FileSystem) gin.HandlerFunc {
	return func(c *gin.Context) {
		file, err := fileSystem.Open("index.html")
		if err != nil {
			backendRootHandler(c)
			return
		}
		_ = file.Close()

		c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
		c.FileFromFS("index.html", fileSystem)
	}
}

func apiAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !config.IsInitialized() {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "system not initialized"})
			return
		}

		if !checkCookie(c) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		c.Next()
	}
}

func backendRootHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"name":    "NasNotify",
		"version": Version,
		"status":  "backend-only",
		"hint":    "Open the packaged native-app frontend instead of the backend service root.",
	})
}

func proxyWechatQRCode(c *gin.Context) {
	qr, err := notify.GetClawBotQRCode()
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	content := strings.TrimSpace(qr.URL)
	if content == "" {
		content = strings.TrimSpace(qr.QRCode)
	}
	if content == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "wechat qrcode content unavailable"})
		return
	}

	png, err := qrcode.Encode(content, qrcode.Medium, 320)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.Header("Cache-Control", "no-store, max-age=0")
	c.Header("Pragma", "no-cache")
	c.Data(http.StatusOK, "image/png", png)
}
