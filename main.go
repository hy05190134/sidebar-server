package main

import (
	"net/http"

	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

func init() {
	// 加载 .env 文件（如果存在）
	if err := godotenv.Load(); err != nil {
		// .env 文件不存在时忽略错误，使用系统环境变量
		logger.Info("未找到 .env 文件，使用系统环境变量")
	}

	// 初始化数据库
	if err := initDatabase(); err != nil {
		logger.Warn("数据库初始化失败，suggestion 关联功能将不可用", zap.Error(err))
	}
}

func main() {
	// 创建 WebSocket Hub
	hub := NewWeComHub()

	// 在 goroutine 中运行 hub
	go hub.Run()

	// 设置路由
	http.HandleFunc("/ws/wecom", WeComWebSocketHandler(hub))
	http.HandleFunc("/api/wx-config", WeComConfigHandler)

	// 启动 HTTP 服务器
	port := ":8080"
	logger.Info("WebSocket 服务器启动", zap.String("port", port))
	if err := http.ListenAndServe(port, nil); err != nil {
		logger.Fatal("服务器启动失败", zap.Error(err))
	}
}
