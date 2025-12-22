package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	mathrand "math/rand"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// generateNonceStr 生成随机字符串
func generateNonceStr(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[mathrand.Intn(len(charset))]
	}
	return string(b)
}

// generateSignature 生成企业微信签名
// 签名算法：将 jsapi_ticket、noncestr、timestamp、url 按字典序排序后拼接，然后进行 SHA1 加密
func generateSignature(jsapiTicket, nonceStr string, timestamp int64, url string) string {
	// 按字典序排序
	params := []string{
		fmt.Sprintf("jsapi_ticket=%s", jsapiTicket),
		fmt.Sprintf("noncestr=%s", nonceStr),
		fmt.Sprintf("timestamp=%d", timestamp),
		fmt.Sprintf("url=%s", url),
	}
	sort.Strings(params)

	// 拼接字符串
	str := strings.Join(params, "&")

	// SHA1 加密
	h := sha1.New()
	h.Write([]byte(str))
	return hex.EncodeToString(h.Sum(nil))
}

// getWeComConfig 获取企业微信配置
func getWeComConfig(r *http.Request) (*WeComConfig, error) {
	// 从环境变量读取配置
	corpID := os.Getenv("WECOM_CORP_ID")
	if corpID == "" {
		return nil, errors.New("WECOM_CORP_ID 环境变量未设置")
	}

	corpSecret := os.Getenv("WECOM_CORP_SECRET")
	if corpSecret == "" {
		return nil, errors.New("WECOM_CORP_SECRET 环境变量未设置")
	}

	agentID := os.Getenv("WECOM_AGENT_ID")
	if agentID == "" {
		return nil, errors.New("WECOM_AGENT_ID 环境变量未设置")
	}

	// 自动获取 jsapi_ticket
	jsapiTicket, err := getJSAPITicket(corpID, corpSecret)
	if err != nil {
		return nil, fmt.Errorf("获取 jsapi_ticket 失败: %w", err)
	}

	// 生成时间戳和随机字符串
	timestamp := time.Now().Unix()
	nonceStr := generateNonceStr(16)

	// 获取当前请求的完整 URL
	requestURL := r.URL.Query().Get("url")
	if requestURL == "" {
		// 如果没有提供 url 参数，则从请求头构建
		scheme := "https"
		if r.TLS == nil {
			scheme = "http"
		}

		// 构建完整的 URL，包含协议、主机（IP或域名）、端口和路径
		host := r.Host
		// r.Host 已经包含了端口号（如果有的话），所以直接使用
		// 例如：192.168.1.1:8080 或 example.com:8080

		// 构建 URL，包含查询参数（如果有）
		requestURL = fmt.Sprintf("%s://%s%s", scheme, host, r.URL.Path)
		if r.URL.RawQuery != "" {
			requestURL += "?" + r.URL.RawQuery
		}
	} else {
		// 如果传入了 url 参数，需要去除 fragment (# 后面的部分)
		// 企业微信签名不包含 fragment
		if idx := strings.Index(requestURL, "#"); idx != -1 {
			requestURL = requestURL[:idx]
		}
	}

	// 生成签名
	signature := generateSignature(jsapiTicket, nonceStr, timestamp, requestURL)

	return &WeComConfig{
		CorpID:      corpID,
		AgentID:     agentID,
		Timestamp:   timestamp,
		NonceStr:    nonceStr,
		Signature:   signature,
		JSAPITicket: jsapiTicket,
	}, nil
}

// WeComConfigHandler 处理企业微信配置请求
func WeComConfigHandler(w http.ResponseWriter, r *http.Request) {
	// 设置 CORS 头（如果需要跨域）
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	// 处理 OPTIONS 预检请求
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	// 只允许 GET 请求
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 获取配置
	config, err := getWeComConfig(r)
	if err != nil {
		log.Printf("获取企业微信配置失败: %v", err)
		http.Error(w, fmt.Sprintf("获取配置失败: %v", err), http.StatusInternalServerError)
		return
	}

	// 返回 JSON 响应
	if err := json.NewEncoder(w).Encode(config); err != nil {
		log.Printf("编码响应失败: %v", err)
		http.Error(w, "编码响应失败", http.StatusInternalServerError)
		return
	}
}
