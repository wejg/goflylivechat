package cmd

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/spf13/cobra"
	"github.com/zh-five/xdaemon"
	"goflylivechat/common"
	"goflylivechat/middleware"
	"goflylivechat/models"
	"goflylivechat/router"
	"goflylivechat/tools"
	"goflylivechat/ws"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

var (
	port   string
	daemon bool
)

var serverCmd = &cobra.Command{
	Use:     "server",
	Short:   "Start HTTP service",
	Example: "gochat server -p 8082",
	Run: func(cmd *cobra.Command, args []string) {
		run()
	},
}

func init() {
	serverCmd.PersistentFlags().StringVarP(&port, "port", "p", "8081", "Port to listen on")
	serverCmd.PersistentFlags().BoolVarP(&daemon, "daemon", "d", false, "Run as daemon process")
}

func run() {
	// Daemon mode setup
	if daemon {
		logFilePath := ""
		if dir, err := os.Getwd(); err == nil {
			logFilePath = dir + "/logs/"
		}
		_, err := os.Stat(logFilePath)
		if os.IsNotExist(err) {
			if err := os.MkdirAll(logFilePath, 0777); err != nil {
				log.Println(err.Error())
			}
		}
		d := xdaemon.NewDaemon(logFilePath + "gofly.log")
		d.MaxCount = 10
		d.Run()
	}

	baseServer := "0.0.0.0:" + port
	log.Println("Starting server...\nURL: http://" + baseServer)
	tools.Logger().Println("Starting server...\nURL: http://" + baseServer)

	// 服务必须有可用的数据库才有意义，这里连不上就退出。
	if err := common.ValidateAppChatConfig(); err != nil {
		log.Fatal(err)
	}
	models.MustConnect()
	if common.AppChatEnabled() {
		if err := models.EnsureChatSchema(); err != nil {
			log.Fatal("chat schema: ", err)
		}
		if err := models.EnsureUploadSchema(); err != nil {
			log.Fatal("upload schema: ", err)
		}
	}

	// 默认 release 模式；需要排查问题时设 GIN_MODE=debug。
	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}

	// Gin engine setup
	engine := gin.New()
	engine.Use(gin.Recovery())
	// 上传走磁盘临时文件，不要为了一个大文件先吃 32MB 内存（gin 默认值）。
	engine.MaxMultipartMemory = 8 << 20
	if err := engine.SetTrustedProxies(trustedProxies()); err != nil {
		log.Println("set trusted proxies:", err)
	}
	engine.LoadHTMLGlob("static/templates/*")
	engine.Use(middleware.PrivateUploadGuard)
	engine.Use(middleware.StaticCache("31536000"))
	engine.Use(middleware.Gzip())
	engine.Static("/static", "./static")
	engine.Use(middleware.CrossSite)

	// Middlewares
	engine.Use(middleware.NewMidLogger())

	// Routers
	router.InitViewRouter(engine)
	router.InitApiRouter(engine)

	// Background services
	tools.NewLimitQueue()
	ws.CleanVisitorExpire()
	go ws.WsServerBackend()

	srv := &http.Server{
		Addr:    baseServer,
		Handler: engine,
		// 不设 WriteTimeout：WebSocket 升级后连接被 hijack，
		// 一个全局写超时会在长连接上造成误杀。
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalln("listen:", err)
		}
	}()

	// 优雅关闭：等在途请求收尾，不要硬断。
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Println("server shutdown:", err)
	}
	models.CloseDB()
	log.Println("Server stopped")
}

// trustedProxies 默认不信任任何代理，由 GOFLY_TRUSTED_PROXIES 显式配置。
// 这直接影响 c.ClientIP()，而 ClientIP 是限流和 IP 黑名单的依据。
func trustedProxies() []string {
	value := strings.TrimSpace(os.Getenv("GOFLY_TRUSTED_PROXIES"))
	if value == "" {
		return nil
	}
	proxies := make([]string, 0)
	for _, p := range strings.Split(value, ",") {
		if p = strings.TrimSpace(p); p != "" {
			proxies = append(proxies, p)
		}
	}
	return proxies
}
