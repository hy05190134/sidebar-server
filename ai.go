package main

import (
	"log"
	"time"
)

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
