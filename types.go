package main

import (
	"encoding/json"
	"errors"
	"sync"
	"time"

	"wework-sdk/wework"

	"github.com/gorilla/websocket"
)

var ErrSendBufferFull = errors.New("send buffer is full")

// TokenCache 缓存 access_token 和 jsapi_ticket
type TokenCache struct {
	mu                  sync.RWMutex
	accessToken         string
	jsapiTicket         string // 应用的 jsapi_ticket
	agentJSAPITicket    string // 企业的 jsapi_ticket
	tokenExpireAt       time.Time
	ticketExpireAt      time.Time
	agentTicketExpireAt time.Time
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

// WeComClient 企业微信客户端
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

// WeComMessage 企业微信消息结构
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

// WeComHub WebSocket Hub
type WeComHub struct {
	Clients    map[string]*WeComClient // agentID -> client
	Broadcast  chan []byte
	Register   chan *WeComClient
	Unregister chan *WeComClient
}
