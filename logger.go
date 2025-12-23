package main

import (
	"go.uber.org/zap"
)

var logger *zap.Logger

func init() {
	// 初始化 zap logger，使用开发模式（生产环境可以改为生产模式）
	var err error
	logger, err = zap.NewDevelopment()
	if err != nil {
		panic("初始化 logger 失败: " + err.Error())
	}
}
