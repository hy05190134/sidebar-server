package main

import (
	"log"
	"net/http"

	"github.com/joho/godotenv"
)

func init() {
	// 加载 .env 文件（如果存在）
	if err := godotenv.Load(); err != nil {
		// .env 文件不存在时忽略错误，使用系统环境变量
		log.Println("未找到 .env 文件，使用系统环境变量")
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
	log.Printf("WebSocket 服务器启动在端口 %s", port)
	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatal("服务器启动失败: ", err)
	}
}
