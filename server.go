// websocket_handler.go
package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	mathrand "math/rand"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"wework-sdk/wework"

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
	Conn           *websocket.Conn
	AgentID        string
	ChatID         string
	Send           chan []byte
	mu             sync.Mutex
	weworkSDK      *wework.SDK
	pollSeq        uint64             // 轮询序列号
	pollTicker     *time.Ticker       // 轮询定时器
	pollStop       chan struct{}      // 停止轮询信号
	pollInterval   time.Duration      // 轮询间隔
	pollIntervalCh chan time.Duration // 更新轮询间隔的通道
}

type WeComMessage struct {
	Type            string          `json:"type"`
	AgentID         string          `json:"agent_id"`
	ChatID          string          `json:"chat_id"`
	Content         json.RawMessage `json:"content"`
	SuggestionID    string          `json:"suggestion_id,omitempty"`
	Action          string          `json:"action,omitempty"`
	OriginalContent string          `json:"original_content,omitempty"`
	EditedContent   string          `json:"edited_content,omitempty"`
	MsgID           string          `json:"msg_id,omitempty"`
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

			// 启动轮询获取会话消息
			go client.startPolling()

		case client := <-h.Unregister:
			if _, ok := h.Clients[client.AgentID]; ok {
				delete(h.Clients, client.AgentID)
				close(client.Send)
				// 停止轮询
				client.stopPolling()
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
			Conn:           conn,
			Send:           make(chan []byte, 256),
			pollSeq:        0,
			pollStop:       make(chan struct{}),
			pollInterval:   5 * time.Second,             // 默认5秒轮询一次
			pollIntervalCh: make(chan time.Duration, 1), // 更新轮询间隔的通道
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
		// 请求AI协助（现在通过轮询触发，这里保留作为手动触发入口）
		go c.handleAIAssistanceRequest(msg)

	case "set_poll_interval":
		// 设置轮询间隔
		c.handleSetPollInterval(msg)

	case "get_poll_interval":
		// 获取当前轮询间隔
		c.handleGetPollInterval()

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
	log.Printf("收到AI反馈: agent_id=%s, chat_id=%s, suggestion_id=%s, action=%s, original_content=%s, edited_content=%s",
		c.AgentID, c.ChatID, msg.SuggestionID, msg.Action, msg.OriginalContent, msg.EditedContent)
	// todo: update suggestion_id and action and  originalContent and editedContent into pg databas
}

// startPolling 启动轮询获取会话消息
func (c *WeComClient) startPolling() {
	// 初始化 wework SDK
	corpID := os.Getenv("WECOM_CORP_ID")
	corpSecret := os.Getenv("WECOM_CORP_SECRET")
	if corpID == "" || corpSecret == "" {
		log.Printf("客服 %s 轮询启动失败: 缺少 WECOM_CORP_ID 或 WECOM_CORP_SECRET 环境变量", c.AgentID)
		return
	}

	// 获取会话存档 Secret（可能需要单独的环境变量）
	archiveSecret := os.Getenv("WECOM_ARCHIVE_SECRET")
	if archiveSecret == "" {
		// 如果没有单独的存档 Secret，使用 corpSecret
		archiveSecret = corpSecret
	}

	sdk := wework.NewSDK()

	if err := sdk.Init(corpID, archiveSecret); err != nil {
		log.Printf("客服 %s 初始化 wework SDK 失败: %v", c.AgentID, err)
		sdk.Destroy()
		return
	}

	c.mu.Lock()
	c.weworkSDK = sdk
	c.pollTicker = time.NewTicker(c.pollInterval)
	currentInterval := c.pollInterval
	c.mu.Unlock()

	log.Printf("客服 %s 开始轮询会话消息，间隔: %v", c.AgentID, currentInterval)

	// 立即执行一次
	c.pollChatMessages()

	// 定时轮询
	for {
		select {
		case <-c.pollTicker.C:
			c.pollChatMessages()
		case newInterval := <-c.pollIntervalCh:
			// 更新轮询间隔
			c.mu.Lock()
			if c.pollTicker != nil {
				c.pollTicker.Stop()
			}
			c.pollInterval = newInterval
			c.pollTicker = time.NewTicker(newInterval)
			log.Printf("客服 %s 轮询间隔已更新为: %v", c.AgentID, newInterval)
			c.mu.Unlock()
			// 发送确认消息
			c.SendMessage(map[string]interface{}{
				"type":          "poll_interval_updated",
				"agent_id":      c.AgentID,
				"poll_interval": float64(newInterval) / float64(time.Second),
			})
		case <-c.pollStop:
			log.Printf("客服 %s 停止轮询会话消息", c.AgentID)
			// 销毁 SDK
			c.mu.Lock()
			if c.weworkSDK != nil {
				c.weworkSDK.Destroy()
				c.weworkSDK = nil
			}
			if c.pollTicker != nil {
				c.pollTicker.Stop()
				c.pollTicker = nil
			}
			c.mu.Unlock()
			return
		}
	}
}

// stopPolling 停止轮询
func (c *WeComClient) stopPolling() {
	c.mu.Lock()
	if c.pollTicker != nil {
		c.pollTicker.Stop()
		c.pollTicker = nil
	}
	c.mu.Unlock()

	select {
	case <-c.pollStop:
		// 已经关闭
	default:
		close(c.pollStop)
	}
}

// handleSetPollInterval 处理设置轮询间隔的请求
func (c *WeComClient) handleSetPollInterval(msg WeComMessage) {
	// 解析消息内容，获取间隔时间（单位：秒）
	var intervalData map[string]interface{}
	if err := json.Unmarshal(msg.Content, &intervalData); err != nil {
		log.Printf("客服 %s 解析轮询间隔设置失败: %v", c.AgentID, err)
		c.SendMessage(map[string]interface{}{
			"type":     "poll_interval_error",
			"agent_id": c.AgentID,
			"error":    "无效的间隔设置格式",
		})
		return
	}

	// 获取间隔值（单位：秒）
	intervalSec, ok := intervalData["interval"].(float64)
	if !ok {
		log.Printf("客服 %s 轮询间隔设置缺少 interval 字段", c.AgentID)
		c.SendMessage(map[string]interface{}{
			"type":     "poll_interval_error",
			"agent_id": c.AgentID,
			"error":    "缺少 interval 字段（单位：秒）",
		})
		return
	}

	// 验证间隔范围（最小1秒，最大1小时）
	minInterval := 1 * time.Second
	maxInterval := 1 * time.Hour
	newInterval := time.Duration(intervalSec) * time.Second

	if newInterval < minInterval {
		log.Printf("客服 %s 轮询间隔设置过小: %v，最小值为: %v", c.AgentID, newInterval, minInterval)
		c.SendMessage(map[string]interface{}{
			"type":     "poll_interval_error",
			"agent_id": c.AgentID,
			"error":    fmt.Sprintf("轮询间隔不能小于 %v", minInterval),
		})
		return
	}

	if newInterval > maxInterval {
		log.Printf("客服 %s 轮询间隔设置过大: %v，最大值为: %v", c.AgentID, newInterval, maxInterval)
		c.SendMessage(map[string]interface{}{
			"type":     "poll_interval_error",
			"agent_id": c.AgentID,
			"error":    fmt.Sprintf("轮询间隔不能大于 %v", maxInterval),
		})
		return
	}

	// 检查轮询是否已启动
	c.mu.Lock()
	hasPolling := c.weworkSDK != nil
	c.mu.Unlock()

	if !hasPolling {
		// 如果轮询未启动，只更新配置，不立即生效
		c.mu.Lock()
		c.pollInterval = newInterval
		c.mu.Unlock()
		log.Printf("客服 %s 轮询间隔配置已更新为: %v（轮询未启动，将在启动时生效）", c.AgentID, newInterval)
		c.SendMessage(map[string]interface{}{
			"type":          "poll_interval_updated",
			"agent_id":      c.AgentID,
			"poll_interval": intervalSec,
			"note":          "轮询未启动，将在启动时生效",
		})
		return
	}

	// 发送更新请求到轮询循环
	select {
	case c.pollIntervalCh <- newInterval:
		log.Printf("客服 %s 已发送轮询间隔更新请求: %v", c.AgentID, newInterval)
	default:
		log.Printf("客服 %s 轮询间隔更新通道已满，跳过", c.AgentID)
		c.SendMessage(map[string]interface{}{
			"type":     "poll_interval_error",
			"agent_id": c.AgentID,
			"error":    "更新请求队列已满，请稍后重试",
		})
	}
}

// handleGetPollInterval 处理获取当前轮询间隔的请求
func (c *WeComClient) handleGetPollInterval() {
	c.mu.Lock()
	interval := c.pollInterval
	isPolling := c.weworkSDK != nil
	c.mu.Unlock()

	c.SendMessage(map[string]interface{}{
		"type":          "poll_interval_info",
		"agent_id":      c.AgentID,
		"poll_interval": float64(interval) / float64(time.Second),
		"is_polling":    isPolling,
	})
}

// pollChatMessages 轮询获取会话消息
func (c *WeComClient) pollChatMessages() {
	c.mu.Lock()
	sdk := c.weworkSDK
	seq := c.pollSeq
	c.mu.Unlock()

	if sdk == nil {
		return
	}

	// 获取会话存档数据
	// limit: 一次拉取的消息数量，最大值1000
	// proxy: 代理地址，不需要代理时传空字符串
	// passwd: 代理账号密码，不需要代理时传空字符串
	// timeout: 超时时间，单位秒
	proxy := os.Getenv("WECOM_PROXY")
	if proxy == "" {
		proxy = ""
	}
	passwd := os.Getenv("WECOM_PROXY_PASSWD")
	if passwd == "" {
		passwd = ""
	}
	timeout := 30

	chatData, err := sdk.GetChatData(seq, 100, proxy, passwd, timeout)
	if err != nil {
		log.Printf("客服 %s 获取会话存档失败: %v", c.AgentID, err)
		return
	}

	if chatData.Len == 0 {
		return
	}

	// 解析 JSON 数据
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(chatData.Data), &result); err != nil {
		log.Printf("客服 %s 解析会话存档数据失败: %v", c.AgentID, err)
		return
	}

	// 检查错误码
	if errcode, ok := result["errcode"].(float64); ok && errcode != 0 {
		errmsg := ""
		if msg, ok := result["errmsg"].(string); ok {
			errmsg = msg
		}
		log.Printf("客服 %s 获取会话存档返回错误: errcode=%.0f, errmsg=%s", c.AgentID, errcode, errmsg)
		return
	}

	// 获取聊天数据数组
	chatdata, ok := result["chatdata"].([]interface{})
	if !ok || len(chatdata) == 0 {
		return
	}

	log.Printf("客服 %s 获取到 %d 条新消息", c.AgentID, len(chatdata))

	// 处理每条消息
	maxSeq := seq
	for _, msgItem := range chatdata {
		msgMap, ok := msgItem.(map[string]interface{})
		if !ok {
			continue
		}

		// 更新最大 seq
		if msgSeq, ok := msgMap["seq"].(float64); ok {
			if uint64(msgSeq) > maxSeq {
				maxSeq = uint64(msgSeq)
			}
		}

		// 获取加密的消息字段
		encryptRandomKey, hasKey := msgMap["encrypt_random_key"].(string)
		encryptChatMsg, hasMsg := msgMap["encrypt_chat_msg"].(string)

		if !hasKey || !hasMsg {
			log.Printf("客服 %s 消息缺少加密字段，跳过解密", c.AgentID)
			continue
		}

		// 解密消息
		decryptedMsg, err := decryptChatMessage(encryptRandomKey, encryptChatMsg)
		if err != nil {
			log.Printf("客服 %s 解密消息失败: %v", c.AgentID, err)
			continue
		}

		// 解析解密后的消息 JSON
		var decryptedMsgData map[string]interface{}
		if err := json.Unmarshal([]byte(decryptedMsg), &decryptedMsgData); err != nil {
			log.Printf("客服 %s 解析解密后的消息失败: %v", c.AgentID, err)
			continue
		}

		// 检查是否是当前会话的消息
		// 根据企业微信文档，解密后的消息包含 chatid 字段
		// 可以通过 chatid 判断是否属于当前会话
		chatID := ""
		if id, ok := decryptedMsgData["from"].(string); ok {
			chatID = id
		}

		// 如果 chatID 不匹配，跳过此消息
		if chatID != "" && chatID != c.ChatID {
			log.Printf("客服 %s 消息 chatid=%s 不匹配当前会话 chat_id=%s，跳过", c.AgentID, chatID, c.ChatID)
			continue
		}

		log.Printf("客服 %s 解密消息成功，chatid=%s", c.AgentID, chatID)

		// 检查消息类型
		msgType, ok := decryptedMsgData["msgtype"].(string)
		if !ok {
			log.Printf("客服 %s 消息类型字段缺失或格式错误，跳过", c.AgentID)
			continue
		}

		// 构建消息内容用于 AI 协助请求（使用解密后的消息）
		var msgContent []byte
		switch msgType {
		case "text":
			// 文本消息，提取 content 字段
			if content, ok := decryptedMsgData["content"].(string); ok {
				msgContent = []byte(content)
			} else {
				// 如果 content 不是字符串，尝试序列化整个消息
				msgContent, _ = json.Marshal(decryptedMsgData)
			}
		default:
			log.Printf("客服 %s 收到类型消息: %s, 暂不支持，跳过", c.AgentID, msgType)
			continue
		}

		// 触发 AI 协助请求
		aiMsg := WeComMessage{
			Type:    "ai_assistance_request",
			AgentID: c.AgentID,
			ChatID:  c.ChatID,
			Content: msgContent,
		}
		if msgID, ok := msgMap["msgid"].(string); ok {
			aiMsg.MsgID = msgID
		}

		go c.handleAIAssistanceRequest(aiMsg)
	}

	// 更新 seq
	c.mu.Lock()
	c.pollSeq = maxSeq
	c.mu.Unlock()
}

// handleAIAssistanceRequest 处理AI协助请求
func (c *WeComClient) handleAIAssistanceRequest(msg WeComMessage) {
	log.Printf("收到AI协助请求: agent_id=%s, chat_id=%s", c.AgentID, c.ChatID)

	// 模拟 AI 处理延迟
	time.Sleep(500 * time.Millisecond)

	// msg.Content 为 string 类型，直接使用
	log.Printf("AI协助请求 context: %s", string(msg.Content))

	// 生成模拟的 AI 协助响应，返回 text 字符串、confidence 和 suggestion_id
	assistanceResponse := map[string]interface{}{
		"type":          "ai_suggestion",
		"agent_id":      c.AgentID,
		"chat_id":       c.ChatID,
		"msg_id":        msg.MsgID,
		"suggestion_id": "sug_001",
		"text":          "您好，请问有什么可以帮助您的吗？根据您的情况，建议您先检查一下相关设置。",
		"confidence":    0.95,
	}

	// 发送 AI 协助响应
	if err := c.SendMessage(assistanceResponse); err != nil {
		log.Printf("发送AI协助响应失败: %v", err)
	} else {
		log.Printf("已发送AI协助响应给客服 %s", c.AgentID)
	}

	// todo: insert new suggestion record into pg database
}

// generateNonceStr 生成随机字符串
func generateNonceStr(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[mathrand.Intn(len(charset))]
	}
	return string(b)
}

// decryptRSAKey 使用 RSA 私钥解密 encrypt_random_key
func decryptRSAKey(encryptedKey string, privateKeyPath string) (string, error) {
	// 读取私钥文件
	privateKeyData, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return "", fmt.Errorf("读取私钥文件失败: %w", err)
	}

	// 解析 PEM 格式的私钥
	block, _ := pem.Decode(privateKeyData)
	if block == nil {
		return "", errors.New("解析私钥失败: 不是有效的 PEM 格式")
	}

	// 解析 PKCS1 或 PKCS8 格式的私钥
	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		// 尝试 PKCS8 格式
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return "", fmt.Errorf("解析私钥失败: %w", err)
		}
		var ok bool
		privateKey, ok = key.(*rsa.PrivateKey)
		if !ok {
			return "", errors.New("私钥不是 RSA 格式")
		}
	}

	// Base64 解码加密的密钥
	encryptedBytes, err := base64.StdEncoding.DecodeString(encryptedKey)
	if err != nil {
		return "", fmt.Errorf("Base64 解码失败: %w", err)
	}

	// RSA 解密
	decryptedBytes, err := rsa.DecryptPKCS1v15(rand.Reader, privateKey, encryptedBytes)
	if err != nil {
		return "", fmt.Errorf("RSA 解密失败: %w", err)
	}

	return string(decryptedBytes), nil
}

// decryptChatMessage 解密会话消息
func decryptChatMessage(encryptRandomKey, encryptChatMsg string) (string, error) {
	// 获取 RSA 私钥路径
	privateKeyPath := os.Getenv("WECOM_RSA_PRIVATE_KEY_PATH")
	if privateKeyPath == "" {
		return "", errors.New("WECOM_RSA_PRIVATE_KEY_PATH 环境变量未设置")
	}

	// 使用 RSA 私钥解密 encrypt_random_key
	decryptedKey, err := decryptRSAKey(encryptRandomKey, privateKeyPath)
	if err != nil {
		return "", fmt.Errorf("解密 encrypt_random_key 失败: %w", err)
	}

	// 使用解密后的 key 和 encrypt_chat_msg 调用 DecryptData
	decryptedMsg, err := wework.DecryptData(decryptedKey, encryptChatMsg)
	if err != nil {
		return "", fmt.Errorf("解密消息失败: %w", err)
	}

	return decryptedMsg, nil
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

	// 使用微信服务端返回的实际过期时间
	// 提前10%的时间过期，确保安全（最少提前60秒）
	expiresIn := result.ExpiresIn
	if expiresIn <= 0 {
		// 如果未返回过期时间，默认使用7200秒（2小时）
		expiresIn = 7200
		log.Printf("警告: access_token 未返回过期时间，使用默认值 7200 秒")
	}

	// 提前10%的时间过期，但最少提前60秒
	earlyExpire := expiresIn / 10
	if earlyExpire < 60 {
		earlyExpire = 60
	}
	expireAt := time.Now().Add(time.Duration(expiresIn-earlyExpire) * time.Second)

	tokenCache.mu.Lock()
	tokenCache.accessToken = result.AccessToken
	tokenCache.tokenExpireAt = expireAt
	tokenCache.mu.Unlock()

	log.Printf("access_token 已缓存，过期时间: %v (微信返回有效期: %d 秒)", expireAt, expiresIn)

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

	// 使用微信服务端返回的实际过期时间
	// 提前10%的时间过期，确保安全（最少提前60秒）
	expiresIn := result.ExpiresIn
	if expiresIn <= 0 {
		// 如果未返回过期时间，默认使用7200秒（2小时）
		expiresIn = 7200
		log.Printf("警告: jsapi_ticket 未返回过期时间，使用默认值 7200 秒")
	}

	// 提前10%的时间过期，但最少提前60秒
	earlyExpire := expiresIn / 10
	if earlyExpire < 60 {
		earlyExpire = 60
	}
	expireAt := time.Now().Add(time.Duration(expiresIn-earlyExpire) * time.Second)

	tokenCache.mu.Lock()
	tokenCache.jsapiTicket = result.Ticket
	tokenCache.ticketExpireAt = expireAt
	tokenCache.mu.Unlock()

	log.Printf("jsapi_ticket 已缓存，过期时间: %v (微信返回有效期: %d 秒)", expireAt, expiresIn)

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
