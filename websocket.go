package main

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// NewWeComHub 创建新的 WebSocket Hub
func NewWeComHub() *WeComHub {
	return &WeComHub{
		Clients:    make(map[string]*WeComClient),
		Broadcast:  make(chan []byte, 256),
		Register:   make(chan *WeComClient),
		Unregister: make(chan *WeComClient),
	}
}

// Run 运行 Hub
func (h *WeComHub) Run() {
	for {
		select {
		case client := <-h.Register:
			h.Clients[client.AgentID] = client
			logger.Info("客服已连接", zap.String("agent_id", client.AgentID))

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
				logger.Info("客服已断开", zap.String("agent_id", client.AgentID))
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

// SendMessage 发送消息
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

// WeComWebSocketHandler WebSocket HTTP 处理器
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
			logger.Error("WebSocket升级失败", zap.Error(err))
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

// readPump 读取消息
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
				logger.Error("读取错误", zap.Error(err))
			}
			break
		}

		c.handleMessage(message, hub)
	}
}

// handleMessage 处理消息
func (c *WeComClient) handleMessage(data []byte, hub *WeComHub) {
	var msg WeComMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		logger.Error("解析消息失败", zap.Error(err))
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
		logger.Info("客服发送了消息", zap.String("agent_id", c.AgentID))

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

// writePump 写入消息
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
