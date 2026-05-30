package main

import (
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"nasnotify-go/internal/config"
)

var Version = "v2026.05.01"

func main() {
	config.InitConfig()
	migrateLegacyAdminPassword()
	ensureRuntimeDirs()

	if !config.IsInitialized() {
		ensureSetupToken()
	}

	go runTasksLoop()
	go runSystemStatusTasksLoop()
	go runClawBotCommandLoop()

	server := newHTTPServer(newRouter())
	log.Printf("NasNotify %s started", Version)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server start failed: %v", err)
	}
}

func newRouter() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	if err := r.SetTrustedProxies([]string{"127.0.0.1", "::1"}); err != nil {
		log.Fatalf("configure trusted proxies failed: %v", err)
	}

	registerAppRoutes(r, "/")
	registerAppRoutes(r, "/nasnotify")
	if frontendDir := findFrontendDir(); frontendDir != "" {
		registerFrontendRoutes(r, frontendDir)
	} else {
		r.GET("/", backendRootHandler)
		r.GET("/nasnotify", backendRootHandler)
		r.GET("/nasnotify/", backendRootHandler)
	}

	return r
}

func newHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              ":5080",
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}
