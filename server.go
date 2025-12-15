// websocket_handler.go
package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/joho/godotenv"
)

var ErrSendBufferFull = errors.New("send buffer is full")

// TokenCache 缓存 access_token 和 jsapi_ticket
type TokenCache struct {
	mu             sync.RWMutex
	accessToken    string
	jsapiTicket    string
	tokenExpireAt  time.Time
	ticketExpireAt time.Time
}

var tokenCache = &TokenCache{}

// WeComAPIResponse 企业微信 API 响应结构
type WeComAPIResponse struct {
	ErrCode     int    `json:"errcode"`
	ErrMsg      string `json:"errmsg"`
	AccessToken string `json:"access_token,omitempty"`
	Ticket      string `json:"ticket,omitempty"`
	ExpiresIn   int    `json:"expires_in,omitempty"`
}

// WeComConfig 企业微信配置
type WeComConfig struct {
	CorpID      string `json:"corpId"`
	AgentID     string `json:"agentId"`
	Timestamp   int64  `json:"timestamp"`
	NonceStr    string `json:"nonceStr"`
	Signature   string `json:"signature"`
	JSAPITicket string `json:"-"` // 不返回给前端
}

type WeComClient struct {
	Conn    *websocket.Conn
	AgentID string
	ChatID  string
	Send    chan []byte
	mu      sync.Mutex
}

type WeComMessage struct {
	Type    string          `json:"type"`
	AgentID string          `json:"agent_id"`
	ChatID  string          `json:"chat_id"`
	Content json.RawMessage `json:"content"`
	MsgID   string          `json:"msg_id,omitempty"`
}

type WeComHub struct {
	Clients    map[string]*WeComClient // agentID -> client
	Broadcast  chan []byte
	Register   chan *WeComClient
	Unregister chan *WeComClient
}

func NewWeComHub() *WeComHub {
	return &WeComHub{
		Clients:    make(map[string]*WeComClient),
		Broadcast:  make(chan []byte, 256),
		Register:   make(chan *WeComClient),
		Unregister: make(chan *WeComClient),
	}
}

func (h *WeComHub) Run() {
	for {
		select {
		case client := <-h.Register:
			h.Clients[client.AgentID] = client
			log.Printf("客服 %s 已连接", client.AgentID)

			// 发送连接成功消息
			client.SendMessage(map[string]interface{}{
				"type":     "auth_success",
				"agent_id": client.AgentID,
				"time":     time.Now().Unix(),
			})

		case client := <-h.Unregister:
			if _, ok := h.Clients[client.AgentID]; ok {
				delete(h.Clients, client.AgentID)
				close(client.Send)
				log.Printf("客服 %s 已断开", client.AgentID)
			}

		case message := <-h.Broadcast:
			// 广播消息给所有客户端
			for _, client := range h.Clients {
				select {
				case client.Send <- message:
				default:
					close(client.Send)
					delete(h.Clients, client.AgentID)
				}
			}
		}
	}
}

func (c *WeComClient) SendMessage(data interface{}) error {
	message, err := json.Marshal(data)
	if err != nil {
		return err
	}

	select {
	case c.Send <- message:
		return nil
	default:
		return ErrSendBufferFull
	}
}

// HTTP处理器
func WeComWebSocketHandler(hub *WeComHub) http.HandlerFunc {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true // 生产环境需要严格检查
		},
	}

	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WebSocket升级失败: %v", err)
			return
		}

		client := &WeComClient{
			Conn: conn,
			Send: make(chan []byte, 256),
		}

		// 启动读写协程
		go client.writePump()
		go client.readPump(hub)
	}
}

func (c *WeComClient) readPump(hub *WeComHub) {
	defer func() {
		hub.Unregister <- c
		c.Conn.Close()
	}()

	c.Conn.SetReadLimit(5120)
	c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("读取错误: %v", err)
			}
			break
		}

		c.handleMessage(message, hub)
	}
}

func (c *WeComClient) handleMessage(data []byte, hub *WeComHub) {
	var msg WeComMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		log.Printf("解析消息失败: %v", err)
		return
	}

	switch msg.Type {
	case "auth":
		// 认证消息，设置客户信息
		c.AgentID = msg.AgentID
		c.ChatID = msg.ChatID
		hub.Register <- c

	case "agent_message_sent":
		// 客服发送了消息
		log.Printf("客服 %s 发送了消息", c.AgentID)

		// 触发AI分析后续对话
		go c.triggerNextAIAnalysis(msg)

	case "ai_feedback":
		// AI建议反馈
		c.handleAIFeedback(msg)

	case "ai_assistance_request":
		// 请求AI协助
		go c.handleAIAssistanceRequest(msg)

	case "pong":
		// 心跳响应
		c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	}
}

func (c *WeComClient) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				// 通道关闭
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.Conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// 发送队列中的剩余消息
			n := len(c.Send)
			for i := 0; i < n; i++ {
				w.Write(<-c.Send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			// 发送心跳
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// triggerNextAIAnalysis 触发AI分析后续对话
func (c *WeComClient) triggerNextAIAnalysis(msg WeComMessage) {
	// TODO: 实现AI分析逻辑
	log.Printf("触发AI分析: agent_id=%s, chat_id=%s", c.AgentID, c.ChatID)
}

// handleAIFeedback 处理AI建议反馈
func (c *WeComClient) handleAIFeedback(msg WeComMessage) {
	// TODO: 实现AI反馈处理逻辑
	log.Printf("收到AI反馈: agent_id=%s, chat_id=%s", c.AgentID, c.ChatID)
}

// handleAIAssistanceRequest 处理AI协助请求
func (c *WeComClient) handleAIAssistanceRequest(msg WeComMessage) {
	// TODO: 实现AI协助请求处理逻辑
	log.Printf("收到AI协助请求: agent_id=%s, chat_id=%s", c.AgentID, c.ChatID)
}

// generateNonceStr 生成随机字符串
func generateNonceStr(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

// getAccessToken 获取企业微信 access_token
func getAccessToken(corpID, corpSecret string) (string, error) {
	// 检查缓存
	tokenCache.mu.RLock()
	if tokenCache.accessToken != "" && time.Now().Before(tokenCache.tokenExpireAt) {
		token := tokenCache.accessToken
		tokenCache.mu.RUnlock()
		return token, nil
	}
	tokenCache.mu.RUnlock()

	// 从 API 获取
	apiURL := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/gettoken?corpid=%s&corpsecret=%s",
		url.QueryEscape(corpID), url.QueryEscape(corpSecret))

	resp, err := http.Get(apiURL)
	if err != nil {
		return "", fmt.Errorf("请求 access_token 失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %w", err)
	}

	var result WeComAPIResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析响应失败: %w", err)
	}

	if result.ErrCode != 0 {
		return "", fmt.Errorf("获取 access_token 失败: errcode=%d, errmsg=%s", result.ErrCode, result.ErrMsg)
	}

	// 更新缓存（提前5分钟过期，确保安全）
	expireAt := time.Now().Add(time.Duration(result.ExpiresIn-300) * time.Second)
	tokenCache.mu.Lock()
	tokenCache.accessToken = result.AccessToken
	tokenCache.tokenExpireAt = expireAt
	tokenCache.mu.Unlock()

	return result.AccessToken, nil
}

// getJSAPITicket 获取企业微信 jsapi_ticket
func getJSAPITicket(corpID, corpSecret string) (string, error) {
	// 检查缓存
	tokenCache.mu.RLock()
	if tokenCache.jsapiTicket != "" && time.Now().Before(tokenCache.ticketExpireAt) {
		ticket := tokenCache.jsapiTicket
		tokenCache.mu.RUnlock()
		return ticket, nil
	}
	tokenCache.mu.RUnlock()

	// 先获取 access_token
	accessToken, err := getAccessToken(corpID, corpSecret)
	if err != nil {
		return "", fmt.Errorf("获取 access_token 失败: %w", err)
	}

	// 获取 jsapi_ticket
	apiURL := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/get_jsapi_ticket?access_token=%s", url.QueryEscape(accessToken))

	resp, err := http.Get(apiURL)
	if err != nil {
		return "", fmt.Errorf("请求 jsapi_ticket 失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %w", err)
	}

	var result WeComAPIResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析响应失败: %w", err)
	}

	if result.ErrCode != 0 {
		return "", fmt.Errorf("获取 jsapi_ticket 失败: errcode=%d, errmsg=%s", result.ErrCode, result.ErrMsg)
	}

	// 更新缓存（提前5分钟过期，确保安全）
	expireAt := time.Now().Add(time.Duration(result.ExpiresIn-300) * time.Second)
	tokenCache.mu.Lock()
	tokenCache.jsapiTicket = result.Ticket
	tokenCache.ticketExpireAt = expireAt
	tokenCache.mu.Unlock()

	return result.Ticket, nil
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
		// 如果没有提供 url 参数，则从请求头或请求 URL 构建
		scheme := "https"
		if r.TLS == nil {
			scheme = "http"
		}
		// 移除查询参数，只保留路径部分用于签名
		requestURL = fmt.Sprintf("%s://%s%s", scheme, r.Host, r.URL.Path)
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
