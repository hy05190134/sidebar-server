# Makefile for sidebar-server

# 变量定义
BINARY_NAME=sidebar-server
MAIN_PACKAGE=.
BUILD_DIR=bin
PID_FILE=$(BUILD_DIR)/$(BINARY_NAME).pid
LOG_FILE=$(BUILD_DIR)/$(BINARY_NAME).log
GO=go
GOFMT=gofmt
SDK_DIR=go_sdk
SDK_LIB_PATH=$(shell pwd)/$(SDK_DIR)

# 检测操作系统类型，设置相应的库路径环境变量
UNAME_S := $(shell uname -s)
ifeq ($(UNAME_S),Linux)
	# Linux 使用 LD_LIBRARY_PATH
	export LD_LIBRARY_PATH := $(SDK_LIB_PATH):$(LD_LIBRARY_PATH)
	LIB_PATH_VAR := $(LD_LIBRARY_PATH)
else ifeq ($(UNAME_S),Darwin)
	# macOS 使用 DYLD_LIBRARY_PATH
	export DYLD_LIBRARY_PATH := $(SDK_LIB_PATH):$(DYLD_LIBRARY_PATH)
	LIB_PATH_VAR := $(DYLD_LIBRARY_PATH)
else
	# 其他系统默认使用 LD_LIBRARY_PATH
	export LD_LIBRARY_PATH := $(SDK_LIB_PATH):$(LD_LIBRARY_PATH)
	LIB_PATH_VAR := $(LD_LIBRARY_PATH)
endif

# 颜色定义
CYAN=\033[0;36m
GREEN=\033[0;32m
YELLOW=\033[0;33m
RED=\033[0;31m
NC=\033[0m # No Color

# 默认目标
.DEFAULT_GOAL := help

## help: 显示帮助信息
.PHONY: help
help:
	@echo "$(CYAN)可用命令:$(NC)"
	@echo ""
	@echo "$(GREEN)服务管理:$(NC)"
	@echo "  make build          - 构建可执行文件"
	@echo "  make start          - 启动服务器（后台运行）"
	@echo "  make stop           - 停止服务器"
	@echo "  make restart        - 重启服务器（先构建再重启）"
	@echo "  make status         - 查看服务器状态"
	@echo "  make logs           - 查看服务器日志"
	@echo ""
	@echo "$(GREEN)开发相关:$(NC)"
	@echo "  make run            - 运行服务器（前台运行）"
	@echo "  make clean          - 清理构建文件"
	@echo ""
	@echo "$(GREEN)依赖管理:$(NC)"
	@echo "  make deps           - 下载依赖"
	@echo "  make deps-tidy      - 整理依赖"
	@echo ""
	@echo "$(GREEN)代码质量:$(NC)"
	@echo "  make fmt            - 格式化代码"
	@echo "  make vet            - 运行 go vet"
	@echo ""

## build: 构建可执行文件
.PHONY: build
build:
	@echo "$(CYAN)构建 $(BINARY_NAME)...$(NC)"
	@mkdir -p $(BUILD_DIR)
	@if [ ! -f $(SDK_DIR)/libWeWorkFinanceSdk_C.so ]; then \
		echo "$(YELLOW)警告: 未找到动态链接库 $(SDK_DIR)/libWeWorkFinanceSdk_C.so$(NC)"; \
		echo "$(YELLOW)请确保 wework SDK 的动态链接库文件存在$(NC)"; \
	fi
	@$(GO) build -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PACKAGE)
	@echo "$(GREEN)构建完成: $(BUILD_DIR)/$(BINARY_NAME)$(NC)"
	@if [ "$(UNAME_S)" = "Darwin" ]; then \
		echo "$(GREEN)提示: 运行时需要设置 DYLD_LIBRARY_PATH=$(LIB_PATH_VAR)$(NC)"; \
	else \
		echo "$(GREEN)提示: 运行时需要设置 LD_LIBRARY_PATH=$(LIB_PATH_VAR)$(NC)"; \
	fi

## run: 运行服务器（前台）
.PHONY: run
run:
	@echo "$(CYAN)启动服务器（前台运行）...$(NC)"
	@if [ "$(UNAME_S)" = "Darwin" ]; then \
		echo "$(YELLOW)DYLD_LIBRARY_PATH=$(LIB_PATH_VAR)$(NC)"; \
		DYLD_LIBRARY_PATH=$(LIB_PATH_VAR) $(GO) run $(MAIN_PACKAGE); \
	else \
		echo "$(YELLOW)LD_LIBRARY_PATH=$(LIB_PATH_VAR)$(NC)"; \
		LD_LIBRARY_PATH=$(LIB_PATH_VAR) $(GO) run $(MAIN_PACKAGE); \
	fi

## start: 启动服务器（后台）
.PHONY: start
start: build
	@if [ -f $(PID_FILE) ]; then \
		PID=$$(cat $(PID_FILE)); \
		if ps -p $$PID > /dev/null 2>&1; then \
			echo "$(YELLOW)服务器已在运行中 (PID: $$PID)$(NC)"; \
			exit 1; \
		else \
			rm -f $(PID_FILE); \
		fi; \
	fi
	@echo "$(CYAN)启动服务器（后台运行）...$(NC)"
	@if [ "$(UNAME_S)" = "Darwin" ]; then \
		echo "$(YELLOW)DYLD_LIBRARY_PATH=$(LIB_PATH_VAR)$(NC)"; \
		DYLD_LIBRARY_PATH=$(LIB_PATH_VAR) nohup $(BUILD_DIR)/$(BINARY_NAME) > $(LOG_FILE) 2>&1 & echo $$! > $(PID_FILE); \
	else \
		echo "$(YELLOW)LD_LIBRARY_PATH=$(LIB_PATH_VAR)$(NC)"; \
		LD_LIBRARY_PATH=$(LIB_PATH_VAR) nohup $(BUILD_DIR)/$(BINARY_NAME) > $(LOG_FILE) 2>&1 & echo $$! > $(PID_FILE); \
	fi
	@sleep 1
	@if [ -f $(PID_FILE) ]; then \
		PID=$$(cat $(PID_FILE)); \
		if ps -p $$PID > /dev/null 2>&1; then \
			echo "$(GREEN)服务器已启动 (PID: $$PID)$(NC)"; \
			echo "$(GREEN)日志文件: $(LOG_FILE)$(NC)"; \
		else \
			echo "$(RED)服务器启动失败，请查看日志: $(LOG_FILE)$(NC)"; \
			rm -f $(PID_FILE); \
			exit 1; \
		fi; \
	else \
		echo "$(RED)无法创建 PID 文件$(NC)"; \
		exit 1; \
	fi

## stop: 停止服务器
.PHONY: stop
stop:
	@if [ ! -f $(PID_FILE) ]; then \
		echo "$(YELLOW)服务器未运行（PID 文件不存在）$(NC)"; \
		exit 0; \
	fi
	@PID=$$(cat $(PID_FILE)); \
	if ! ps -p $$PID > /dev/null 2>&1; then \
		echo "$(YELLOW)服务器未运行（进程不存在）$(NC)"; \
		rm -f $(PID_FILE); \
		exit 0; \
	fi
	@echo "$(CYAN)停止服务器 (PID: $$(cat $(PID_FILE)))...$(NC)"
	@kill $$(cat $(PID_FILE)) 2>/dev/null || true
	@for i in 1 2 3 4 5; do \
		if ! ps -p $$(cat $(PID_FILE)) > /dev/null 2>&1; then \
			break; \
		fi; \
		sleep 1; \
	done
	@if ps -p $$(cat $(PID_FILE)) > /dev/null 2>&1; then \
		echo "$(YELLOW)强制停止服务器...$(NC)"; \
		kill -9 $$(cat $(PID_FILE)) 2>/dev/null || true; \
	fi
	@rm -f $(PID_FILE)
	@echo "$(GREEN)服务器已停止$(NC)"

## restart: 重启服务器（先构建再重启）
.PHONY: restart
restart: build stop start
	@echo "$(GREEN)服务器重启完成$(NC)"

## status: 查看服务器状态
.PHONY: status
status:
	@if [ ! -f $(PID_FILE) ]; then \
		echo "$(YELLOW)服务器状态: 未运行$(NC)"; \
		exit 0; \
	fi
	@PID=$$(cat $(PID_FILE)); \
	if ps -p $$PID > /dev/null 2>&1; then \
		echo "$(GREEN)服务器状态: 运行中$(NC)"; \
		echo "$(GREEN)PID: $$PID$(NC)"; \
		echo "$(GREEN)日志文件: $(LOG_FILE)$(NC)"; \
	else \
		echo "$(YELLOW)服务器状态: 未运行（PID 文件存在但进程不存在）$(NC)"; \
		rm -f $(PID_FILE); \
	fi

## logs: 查看服务器日志
.PHONY: logs
logs:
	@if [ ! -f $(LOG_FILE) ]; then \
		echo "$(YELLOW)日志文件不存在: $(LOG_FILE)$(NC)"; \
		exit 0; \
	fi
	@echo "$(CYAN)显示服务器日志（按 Ctrl+C 退出）...$(NC)"
	@tail -f $(LOG_FILE)

## deps: 下载依赖
.PHONY: deps
deps:
	@echo "$(CYAN)下载依赖...$(NC)"
	@$(GO) mod download
	@echo "$(GREEN)依赖下载完成$(NC)"

## deps-tidy: 整理依赖
.PHONY: deps-tidy
deps-tidy:
	@echo "$(CYAN)整理依赖...$(NC)"
	@$(GO) mod tidy
	@echo "$(GREEN)依赖整理完成$(NC)"

## fmt: 格式化代码
.PHONY: fmt
fmt:
	@echo "$(CYAN)格式化代码...$(NC)"
	@$(GOFMT) -w .
	@echo "$(GREEN)代码格式化完成$(NC)"

## vet: 运行 go vet
.PHONY: vet
vet:
	@echo "$(CYAN)运行 go vet...$(NC)"
	@$(GO) vet ./...
	@echo "$(GREEN)go vet 检查完成$(NC)"

## clean: 清理构建文件
.PHONY: clean
clean:
	@echo "$(CYAN)清理构建文件...$(NC)"
	@if [ -f $(PID_FILE) ]; then \
		PID=$$(cat $(PID_FILE)); \
		if ps -p $$PID > /dev/null 2>&1; then \
			echo "$(YELLOW)警告: 服务器正在运行 (PID: $$PID)，请先执行 make stop$(NC)"; \
			exit 1; \
		fi; \
	fi
	@rm -rf $(BUILD_DIR)
	@$(GO) clean
	@echo "$(GREEN)清理完成$(NC)"

