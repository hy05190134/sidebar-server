package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"wework-sdk/wework"
)

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

	// 按 chatId 分类聚合消息
	type MessageInfo struct {
		Content []byte
		MsgID   string
		Seq     uint64
	}
	chatMessages := make(map[string][]MessageInfo) // chatId -> messages

	// 处理每条消息，按 chatId 分类
	maxSeq := seq
	for _, msgItem := range chatdata {
		msgMap, ok := msgItem.(map[string]interface{})
		if !ok {
			continue
		}

		// 更新最大 seq
		var msgSeq uint64
		if seqVal, ok := msgMap["seq"].(float64); ok {
			msgSeq = uint64(seqVal)
			if msgSeq > maxSeq {
				maxSeq = msgSeq
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

		// 获取 chatId
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

		// 获取 msgid
		msgID := ""
		if id, ok := msgMap["msgid"].(string); ok {
			msgID = id
		}

		// 按 chatId 聚合消息
		if chatID == "" {
			chatID = c.ChatID // 如果没有 chatID，使用当前会话的 chatID
		}
		chatMessages[chatID] = append(chatMessages[chatID], MessageInfo{
			Content: msgContent,
			MsgID:   msgID,
			Seq:     msgSeq,
		})
	}

	// 并发发送每个 chatId 的聚合消息给 AI
	var wg sync.WaitGroup
	for chatID, messages := range chatMessages {
		if len(messages) == 0 {
			continue
		}

		wg.Add(1)
		go func(cid string, msgs []MessageInfo) {
			defer wg.Done()

			// 聚合多条消息内容
			var aggregatedContent []byte
			if len(msgs) == 1 {
				// 如果只有一条消息，直接使用
				aggregatedContent = msgs[0].Content
			} else {
				// 多条消息，合并为 JSON 数组
				contents := make([]string, 0, len(msgs))
				for _, msg := range msgs {
					contents = append(contents, string(msg.Content))
				}
				aggregatedContent, _ = json.Marshal(contents)
			}

			// 使用第一条消息的 msgID（或可以合并所有 msgID）
			msgID := ""
			if len(msgs) > 0 {
				msgID = msgs[0].MsgID
			}

			// 触发 AI 协助请求
			aiMsg := WeComMessage{
				Type:    "ai_assistance_request",
				AgentID: c.AgentID,
				ChatID:  cid,
				Content: aggregatedContent,
				MsgID:   msgID,
			}

			log.Printf("客服 %s 发送聚合消息给 AI: chat_id=%s, 消息数量=%d", c.AgentID, cid, len(msgs))
			c.handleAIAssistanceRequest(aiMsg)
		}(chatID, messages)
	}

	// 等待所有 AI 请求完成（可选，根据需求决定是否等待）
	wg.Wait()

	// 更新 seq
	c.mu.Lock()
	c.pollSeq = maxSeq
	c.mu.Unlock()
}
