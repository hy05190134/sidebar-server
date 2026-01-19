package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// AgentCallContent Agent调用内容
type AgentCallContent struct {
	Type    string      `json:"type"`    // 内容类型
	Content interface{} `json:"content"` // 事件内容
}

// AgentInfo Agent信息
type AgentInfo struct {
	AgentID          string `json:"agent_id"`          // Agent ID
	CustomID         string `json:"custom_id"`         // 自定义ID
	PublishedVersion string `json:"published_version"` // 发布版本
	URL              string `json:"url"`               // Agent URL
	Type             string `json:"type"`              // Agent类型
	AgentProviderID  int    `json:"agent_provider_id"` // Agent提供商ID
	Description      string `json:"description"`       // Agent描述
	Name             string `json:"name"`              // Agent名称
}

// AgentCallEvent Agent调用事件请求
type AgentCallEvent struct {
	Type             string             `json:"type"`                // 事件类型，必填
	Contents         []AgentCallContent `json:"contents"`            // 事件内容，必填
	Agents           []AgentInfo        `json:"agents,omitempty"`    // Agent列表
	Tools            []interface{}      `json:"tools,omitempty"`     // 工具列表
	CallerInstanceID int64              `json:"caller_instance_id"`  // 调用者实例ID
	CallerType       string             `json:"caller_type"`         // 调用者类型
	UserID           string             `json:"userId,omitempty"`    // 用户ID（保留向后兼容）
	SessionID        int                `json:"sessionId,omitempty"` // 会话ID（保留向后兼容）
	Timestamp        int64              `json:"timestamp,omitempty"` // 事件时间戳（保留向后兼容）
}

// AgentResponse Agent响应
type AgentResponse struct {
	Code      int                    `json:"code"`       // 状态码
	SessionID int                    `json:"session_id"` // 会话ID
	Message   string                 `json:"message"`    // 消息
	Data      map[string]interface{} `json:"data"`       // 数据
}

// triggerNextAIAnalysis 触发AI分析后续对话
func (c *WeComClient) triggerNextAIAnalysis(msg WeComMessage) {
	// TODO: 实现AI分析逻辑
	logger.Info("触发AI分析", zap.String("agent_id", c.AgentID), zap.String("chat_id", c.ChatID))
}

// handleAIFeedback 处理AI建议反馈
func (c *WeComClient) handleAIFeedback(msg WeComMessage) {
	logger.Info("收到AI反馈",
		zap.String("agent_id", c.AgentID),
		zap.String("chat_id", c.ChatID),
		zap.String("suggestion_id", msg.SuggestionID),
		zap.String("action", msg.Action),
		zap.String("original_content", msg.OriginalContent),
		zap.String("edited_content", msg.EditedContent))

	// 验证必要字段
	if msg.SuggestionID == "" {
		logger.Warn("suggestion_id 为空，跳过数据库更新", zap.String("agent_id", c.AgentID))
		return
	}

	if msg.Action == "" {
		logger.Warn("action 为空，跳过数据库更新", zap.String("agent_id", c.AgentID), zap.String("suggestion_id", msg.SuggestionID))
		return
	}

	// 更新数据库中的反馈信息
	if err := updateSuggestionFeedback(msg.SuggestionID, msg.Action, msg.OriginalContent, msg.EditedContent); err != nil {
		logger.Error("更新 suggestion 反馈信息失败",
			zap.String("agent_id", c.AgentID),
			zap.String("chat_id", c.ChatID),
			zap.String("suggestion_id", msg.SuggestionID),
			zap.String("action", msg.Action),
			zap.Error(err))
	} else {
		logger.Info("成功更新 suggestion 反馈信息",
			zap.String("agent_id", c.AgentID),
			zap.String("chat_id", c.ChatID),
			zap.String("suggestion_id", msg.SuggestionID),
			zap.String("action", msg.Action),
			zap.String("original_content", msg.OriginalContent),
			zap.String("edited_content", msg.EditedContent))
	}
}

// handleAIAssistanceRequest 处理AI协助请求
func (c *WeComClient) handleAIAssistanceRequest(msg WeComMessage) {
	logger.Info("收到AI协助请求", zap.String("agent_id", c.AgentID), zap.String("chat_id", c.ChatID))

	// msg.Content 为 string 类型，直接使用
	logger.Debug("AI协助请求 context", zap.String("agent_id", c.AgentID), zap.String("chat_id", c.ChatID), zap.String("context", string(msg.Content)))

	// 调用 Agent API 获取建议
	agentResp, err := c.callAgentAPI(msg.Content)
	if err != nil {
		logger.Error("调用 Agent API 失败",
			zap.String("agent_id", c.AgentID),
			zap.String("chat_id", c.ChatID),
			zap.Error(err))
		return
	}

	// 从 Agent 响应中提取建议文本
	suggestionText := ""
	confidence := 0.8

	// 尝试从 data 中提取内容
	// data 结构: { "0": { "type": "text", "content": "..." } }
	if agentResp.Data != nil {
		// 先尝试从 "0" key 获取内容对象
		if contentObj, ok := agentResp.Data["0"].(map[string]interface{}); ok {
			// 从内容对象中提取 content 字段
			if content, ok := contentObj["content"].(string); ok && content != "" {
				suggestionText = content
			}
		} else {
			// 向后兼容：尝试直接从 data 中提取（旧格式）
			if text, ok := agentResp.Data["text"].(string); ok && text != "" {
				suggestionText = text
			} else if response, ok := agentResp.Data["response"].(string); ok && response != "" {
				suggestionText = response
			} else if content, ok := agentResp.Data["content"].(string); ok && content != "" {
				suggestionText = content
			}
		}
	}

	// 如果没有获取到有效文本，记录警告并返回
	if suggestionText == "" {
		logger.Warn("Agent API 未返回有效建议文本",
			zap.String("agent_id", c.AgentID),
			zap.String("chat_id", c.ChatID),
			zap.Any("agent_data", agentResp.Data))
		return
	}

	// 生成 suggestion_id
	suggestionID := fmt.Sprintf("sug_%d", time.Now().UnixNano())

	// 构造 AI 协助响应
	assistanceResponse := map[string]interface{}{
		"type":          "ai_suggestion",
		"agent_id":      c.AgentID,
		"chat_id":       c.ChatID,
		"msg_id":        "",
		"suggestion_id": suggestionID,
		"text":          suggestionText,
		"confidence":    confidence,
	}

	// 发送 AI 协助响应
	if err := c.SendMessage(assistanceResponse); err != nil {
		logger.Error("发送AI协助响应失败", zap.String("agent_id", c.AgentID), zap.Error(err))
		return
	}

	logger.Info("已发送AI协助响应给客服", zap.String("agent_id", c.AgentID))

	// 插入 suggestion 记录到数据库
	msgID := ""
	if err := createSuggestion(suggestionID, c.AgentID, c.ChatID, msgID, suggestionText, confidence); err != nil {
		logger.Error("插入 suggestion 记录失败",
			zap.String("agent_id", c.AgentID),
			zap.String("chat_id", c.ChatID),
			zap.String("suggestion_id", suggestionID),
			zap.Error(err))
	} else {
		logger.Info("成功插入 suggestion 记录",
			zap.String("agent_id", c.AgentID),
			zap.String("chat_id", c.ChatID),
			zap.String("suggestion_id", suggestionID),
			zap.String("msg_id", msgID),
			zap.Float64("confidence", confidence))
	}
}

// callAgentAPI 调用 Agent API
func (c *WeComClient) callAgentAPI(content interface{}) (*AgentResponse, error) {
	// Agent API 地址
	agentURL := "http://192.168.201.28:8080/customer_support/assist"

	// 构造请求体
	requestBody := AgentCallEvent{
		Type: "user_input",
		Contents: []AgentCallContent{
			{
				Type:    "text",
				Content: content,
			},
		},
		Agents: []AgentInfo{
			{
				AgentID:          "customer-support-agent",
				CustomID:         "customer-support-agent",
				PublishedVersion: "1.0.0",
				URL:              "local",
				Type:             "",
				AgentProviderID:  0,
				Description:      "",
				Name:             "",
			},
		},
		Tools:            []interface{}{},
		CallerInstanceID: time.Now().UnixNano() / 1000, // 微秒级时间戳
		CallerType:       "user",
		UserID:           c.ChatID,
		Timestamp:        time.Now().Unix(),
	}

	// 序列化请求体
	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求体失败: %w", err)
	}

	logger.Debug("调用 Agent API",
		zap.String("agent_id", c.AgentID),
		zap.String("url", agentURL),
		zap.String("request", string(jsonData)))

	// 创建 HTTP 请求
	req, err := http.NewRequest("POST", agentURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// 发送请求
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("发送请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	logger.Debug("收到 Agent API 响应",
		zap.String("agent_id", c.AgentID),
		zap.Int("status_code", resp.StatusCode),
		zap.String("response", string(respBody)))

	// 检查 HTTP 状态码
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Agent API 返回错误状态码: %d, 响应: %s", resp.StatusCode, string(respBody))
	}

	// 解析响应
	var agentResp AgentResponse
	if err := json.Unmarshal(respBody, &agentResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	// 检查业务状态码
	if agentResp.Code != 200 {
		return nil, fmt.Errorf("Agent API 返回错误: code=%d, message=%s", agentResp.Code, agentResp.Message)
	}

	logger.Info("成功调用 Agent API",
		zap.String("agent_id", c.AgentID),
		zap.Int("session_id", agentResp.SessionID))

	return &agentResp, nil
}
