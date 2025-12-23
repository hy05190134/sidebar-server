package main

import (
	"time"

	"go.uber.org/zap"
)

// triggerNextAIAnalysis 触发AI分析后续对话
func (c *WeComClient) triggerNextAIAnalysis(msg WeComMessage) {
	// TODO: 实现AI分析逻辑
	logger.Info("触发AI分析", zap.String("agent_id", c.AgentID), zap.String("chat_id", c.ChatID))
}

// handleAIFeedback 处理AI建议反馈
func (c *WeComClient) handleAIFeedback(msg WeComMessage) {
	// TODO: 实现AI反馈处理逻辑
	logger.Info("收到AI反馈",
		zap.String("agent_id", c.AgentID),
		zap.String("chat_id", c.ChatID),
		zap.String("suggestion_id", msg.SuggestionID),
		zap.String("action", msg.Action),
		zap.String("original_content", msg.OriginalContent),
		zap.String("edited_content", msg.EditedContent))
	// todo: update suggestion_id and action and  originalContent and editedContent into pg databas
}

// handleAIAssistanceRequest 处理AI协助请求
func (c *WeComClient) handleAIAssistanceRequest(msg WeComMessage) {
	logger.Info("收到AI协助请求", zap.String("agent_id", c.AgentID), zap.String("chat_id", c.ChatID))

	// 模拟 AI 处理延迟
	time.Sleep(500 * time.Millisecond)

	// msg.Content 为 string 类型，直接使用
	logger.Debug("AI协助请求 context", zap.String("agent_id", c.AgentID), zap.String("chat_id", c.ChatID), zap.String("context", string(msg.Content)))

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
		logger.Error("发送AI协助响应失败", zap.String("agent_id", c.AgentID), zap.Error(err))
	} else {
		logger.Info("已发送AI协助响应给客服", zap.String("agent_id", c.AgentID))
	}

	// todo: insert new suggestion record into pg database
}
