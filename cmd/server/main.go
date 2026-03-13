package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/wnyr/openapi2word/internal/api"
)

func main() {
	// 解析命令行参数
	port := flag.Int("port", 8080, "服务器端口")
	flag.Parse()
	// 设置 Gin 模式
	mode := os.Getenv("GIN_MODE")
	if mode == "" {
		gin.SetMode(gin.ReleaseMode)
	}
	// 创建 Gin 引擎
	r := gin.Default()
	// 配置 CORS
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	})

	// 注册 API 路由
	api.RegisterRoutes(r)
	// 健康检查端点
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
		})
	})

	//r.Run(":8888")
	// 启动服务器
	addr := fmt.Sprintf(":%d", *port)
	log.Printf("🚀 服务器启动在 http://localhost%s", addr)

	if err := r.Run(addr); err != nil {
		log.Fatalf("❌ 服务器启动失败：%v", err)
	}
}
