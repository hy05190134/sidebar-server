package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"wework-sdk/wework"

	"go.uber.org/zap"
)

// startPolling 启动轮询获取会话消息
func (c *WeComClient) startPolling() {
	// 初始化 wework SDK
	corpID := os.Getenv("WECOM_CORP_ID")
	corpSecret := os.Getenv("WECOM_CORP_SECRET")
	if corpID == "" || corpSecret == "" {
		logger.Warn("客服轮询启动失败: 缺少环境变量", zap.String("agent_id", c.AgentID))
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
		logger.Error("客服初始化 wework SDK 失败", zap.String("agent_id", c.AgentID), zap.Error(err))
		sdk.Destroy()
		return
	}

	c.mu.Lock()
	c.weworkSDK = sdk
	c.pollTicker = time.NewTicker(c.pollInterval)
	currentInterval := c.pollInterval
	c.mu.Unlock()

	logger.Info("客服开始轮询会话消息", zap.String("agent_id", c.AgentID), zap.Duration("interval", currentInterval))

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
			logger.Info("客服轮询间隔已更新", zap.String("agent_id", c.AgentID), zap.Duration("interval", newInterval))
			c.mu.Unlock()
			// 发送确认消息
			c.SendMessage(map[string]interface{}{
				"type":          "poll_interval_updated",
				"agent_id":      c.AgentID,
				"poll_interval": float64(newInterval) / float64(time.Second),
			})
		case <-c.pollStop:
			logger.Info("客服停止轮询会话消息", zap.String("agent_id", c.AgentID))
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
		logger.Error("客服解析轮询间隔设置失败", zap.String("agent_id", c.AgentID), zap.Error(err))
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
		logger.Warn("客服轮询间隔设置缺少 interval 字段", zap.String("agent_id", c.AgentID))
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
		logger.Warn("客服轮询间隔设置过小", zap.String("agent_id", c.AgentID), zap.Duration("interval", newInterval), zap.Duration("min_interval", minInterval))
		c.SendMessage(map[string]interface{}{
			"type":     "poll_interval_error",
			"agent_id": c.AgentID,
			"error":    fmt.Sprintf("轮询间隔不能小于 %v", minInterval),
		})
		return
	}

	if newInterval > maxInterval {
		logger.Warn("客服轮询间隔设置过大", zap.String("agent_id", c.AgentID), zap.Duration("interval", newInterval), zap.Duration("max_interval", maxInterval))
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
		logger.Info("客服轮询间隔配置已更新（轮询未启动，将在启动时生效）", zap.String("agent_id", c.AgentID), zap.Duration("interval", newInterval))
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
		logger.Info("客服已发送轮询间隔更新请求", zap.String("agent_id", c.AgentID), zap.Duration("interval", newInterval))
	default:
		logger.Warn("客服轮询间隔更新通道已满，跳过", zap.String("agent_id", c.AgentID))
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

// downloadVoiceFile 下载语音文件
func (c *WeComClient) downloadVoiceFile(sdkFileid string) ([]byte, error) {
	c.mu.Lock()
	sdk := c.weworkSDK
	c.mu.Unlock()

	if sdk == nil {
		return nil, fmt.Errorf("wework SDK 未初始化")
	}

	// 获取代理配置
	proxy := os.Getenv("WECOM_PROXY")
	if proxy == "" {
		proxy = ""
	}
	passwd := os.Getenv("WECOM_PROXY_PASSWD")
	if passwd == "" {
		passwd = ""
	}
	timeout := 30

	// 分片下载媒体文件
	var voiceData bytes.Buffer
	indexbuf := ""
	isFinish := false

	for !isFinish {
		mediaData, err := sdk.GetMediaData(indexbuf, sdkFileid, proxy, passwd, timeout)
		if err != nil {
			return nil, fmt.Errorf("获取媒体数据失败: %w", err)
		}

		// 写入缓冲区
		if _, err := voiceData.Write(mediaData.Data); err != nil {
			return nil, fmt.Errorf("写入数据失败: %w", err)
		}

		indexbuf = mediaData.OutIndex
		isFinish = mediaData.IsFinish
	}

	logger.Debug("语音文件下载完成", zap.String("agent_id", c.AgentID), zap.Int("size", voiceData.Len()))
	return voiceData.Bytes(), nil
}

// convertVoiceToText 将语音转换为文本
func (c *WeComClient) convertVoiceToText(voiceData []byte) (string, error) {
	// 获取 access_token
	corpID := os.Getenv("WECOM_CORP_ID")
	corpSecret := os.Getenv("WECOM_CORP_SECRET")
	if corpID == "" || corpSecret == "" {
		return "", fmt.Errorf("缺少 WECOM_CORP_ID 或 WECOM_CORP_SECRET 环境变量")
	}

	accessToken, err := getAccessToken(corpID, corpSecret)
	if err != nil {
		return "", fmt.Errorf("获取 access_token 失败: %w", err)
	}

	// 调用企业微信语音识别 API
	// 注意：这里使用企业微信的语音识别接口
	// 如果企业微信没有提供，可以使用第三方服务（如百度、腾讯等）
	text, err := recognizeVoiceWithWeCom(accessToken, voiceData)
	if err != nil {
		logger.Warn("企业微信语音识别失败，尝试其他方式", zap.String("agent_id", c.AgentID), zap.Error(err))
		// 可以在这里添加其他语音识别服务的调用
		return "", fmt.Errorf("语音转文本失败: %w", err)
	}

	return text, nil
}

// recognizeVoiceWithWeCom 使用企业微信 API 进行语音识别
func recognizeVoiceWithWeCom(accessToken string, voiceData []byte) (string, error) {
	// 企业微信语音识别 API
	// 注意：企业微信可能没有直接的语音识别 API，这里需要根据实际情况调整
	// 如果有第三方语音识别服务，可以在这里调用

	// 检查是否配置了第三方语音识别服务
	voiceAPIURL := os.Getenv("VOICE_RECOGNITION_API_URL")
	if voiceAPIURL != "" {
		return recognizeVoiceWithThirdParty(voiceAPIURL, voiceData)
	}

	// 尝试使用企业微信可能的语音识别接口（如果有的话）
	// 这里先返回错误，提示需要配置
	return "", fmt.Errorf("未配置语音识别服务，请设置 VOICE_RECOGNITION_API_URL 环境变量")
}

// linkSuggestionToMessage 将客服消息与 suggestion 进行关联
// 根据消息内容匹配 suggestion 表中的 original_content 或 edited_content
func (c *WeComClient) linkSuggestionToMessage(agentID, chatID, msgID, content string, msgTime time.Time) {
	// 从环境变量获取查询条数，默认 10 条
	queryLimit := 10
	if limitStr := os.Getenv("SUGGESTION_QUERY_LIMIT"); limitStr != "" {
		if limit, err := fmt.Sscanf(limitStr, "%d", &queryLimit); err != nil || limit != 1 {
			queryLimit = 10
		}
	}

	// 查询 suggestion 表
	suggestions, err := findSuggestionsByContent(agentID, chatID, content, msgTime, queryLimit)
	if err != nil {
		logger.Error("查询 suggestion 失败",
			zap.String("agent_id", agentID),
			zap.String("chat_id", chatID),
			zap.String("msg_id", msgID),
			zap.Error(err))
		return
	}

	if len(suggestions) == 0 {
		logger.Debug("未找到匹配的 suggestion",
			zap.String("agent_id", agentID),
			zap.String("chat_id", chatID),
			zap.String("msg_id", msgID),
			zap.String("content", content))
		return
	}

	// 更新相似度最高的 suggestion（已按相似度从高到低排序）
	suggestion := suggestions[0]

	// 记录匹配到的所有 suggestion 的相似度（用于调试）
	if len(suggestions) > 1 {
		similarities := make([]float64, len(suggestions))
		for i, s := range suggestions {
			similarities[i] = s.Similarity
		}
		logger.Debug("找到多条匹配的 suggestion，选择相似度最高的",
			zap.String("agent_id", agentID),
			zap.String("chat_id", chatID),
			zap.String("msg_id", msgID),
			zap.Float64s("all_similarities", similarities),
			zap.Float64("selected_similarity", suggestion.Similarity),
			zap.String("selected_suggestion_id", suggestion.SuggestionID))
	}

	if err := updateSuggestionMsgID(suggestion.SuggestionID, msgID, suggestion.Similarity); err != nil {
		logger.Error("更新 suggestion msg_id 和相似率失败",
			zap.String("agent_id", agentID),
			zap.String("chat_id", chatID),
			zap.String("msg_id", msgID),
			zap.String("suggestion_id", suggestion.SuggestionID),
			zap.Float64("similarity", suggestion.Similarity),
			zap.Error(err))
		return
	}

	logger.Info("成功关联 suggestion 与消息",
		zap.String("agent_id", agentID),
		zap.String("chat_id", chatID),
		zap.String("msg_id", msgID),
		zap.String("suggestion_id", suggestion.SuggestionID),
		zap.Float64("similarity", suggestion.Similarity),
		zap.String("match_type", suggestion.MatchType),
		zap.Int("matched_count", len(suggestions)))
}

// recognizeVoiceWithThirdParty 使用第三方语音识别服务
func recognizeVoiceWithThirdParty(apiURL string, voiceData []byte) (string, error) {
	// 创建 multipart/form-data 请求
	var requestBody bytes.Buffer

	// 这里需要根据实际的第三方 API 格式来构建请求
	// 示例：使用 multipart form 上传语音文件
	boundary := "----WebKitFormBoundary7MA4YWxkTrZu0gW"

	// 构建 multipart form data
	formData := fmt.Sprintf("--%s\r\n", boundary)
	formData += fmt.Sprintf("Content-Disposition: form-data; name=\"voice\"; filename=\"voice.amr\"\r\n")
	formData += "Content-Type: audio/amr\r\n\r\n"

	requestBody.WriteString(formData)
	requestBody.Write(voiceData)
	requestBody.WriteString(fmt.Sprintf("\r\n--%s--\r\n", boundary))

	// 创建 HTTP 请求
	req, err := http.NewRequest("POST", apiURL, &requestBody)
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %w", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", fmt.Sprintf("multipart/form-data; boundary=%s", boundary))

	// 检查是否需要认证
	apiKey := os.Getenv("VOICE_RECOGNITION_API_KEY")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	// 发送请求
	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求语音识别服务失败: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("语音识别服务返回错误: status=%d, body=%s", resp.StatusCode, string(body))
	}

	// 解析响应（根据实际 API 格式调整）
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析响应失败: %w", err)
	}

	// 提取文本结果（根据实际 API 格式调整字段名）
	text, ok := result["text"].(string)
	if !ok {
		// 尝试其他可能的字段名
		if transcript, ok := result["transcript"].(string); ok {
			text = transcript
		} else if resultText, ok := result["result"].(string); ok {
			text = resultText
		} else {
			return "", fmt.Errorf("响应中未找到文本结果: %s", string(body))
		}
	}

	return text, nil
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
		logger.Error("客服获取会话存档失败", zap.String("agent_id", c.AgentID), zap.Error(err))
		return
	}

	if chatData.Len == 0 {
		return
	}

	// 解析 JSON 数据
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(chatData.Data), &result); err != nil {
		logger.Error("客服解析会话存档数据失败", zap.String("agent_id", c.AgentID), zap.Error(err))
		return
	}

	// 检查错误码
	if errcode, ok := result["errcode"].(float64); ok && errcode != 0 {
		errmsg := ""
		if msg, ok := result["errmsg"].(string); ok {
			errmsg = msg
		}
		logger.Error("客服获取会话存档返回错误", zap.String("agent_id", c.AgentID), zap.Float64("errcode", errcode), zap.String("errmsg", errmsg))
		return
	}

	// 获取聊天数据数组
	chatdata, ok := result["chatdata"].([]interface{})
	if !ok || len(chatdata) == 0 {
		return
	}

	logger.Info("客服获取到新消息", zap.String("agent_id", c.AgentID), zap.Int("count", len(chatdata)))

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
			logger.Warn("客服消息缺少加密字段，跳过解密", zap.String("agent_id", c.AgentID))
			continue
		}

		// 解密消息
		decryptedMsg, err := decryptChatMessage(encryptRandomKey, encryptChatMsg)
		if err != nil {
			logger.Error("客服解密消息失败", zap.String("agent_id", c.AgentID), zap.Error(err))
			continue
		}

		// 解析解密后的消息 JSON
		var decryptedMsgData map[string]interface{}
		if err := json.Unmarshal([]byte(decryptedMsg), &decryptedMsgData); err != nil {
			logger.Error("客服解析解密后的消息失败", zap.String("agent_id", c.AgentID), zap.Error(err))
			continue
		}

		// 获取 chatId
		chatID := ""
		if id, ok := decryptedMsgData["from"].(string); ok {
			chatID = id
		}

		// 如果 chatID 不匹配，跳过此消息
		if chatID != "" && chatID != c.ChatID {
			logger.Debug("客服消息 chatid 不匹配当前会话，跳过", zap.String("agent_id", c.AgentID), zap.String("chat_id", chatID), zap.String("current_chat_id", c.ChatID))
			continue
		}

		logger.Debug("客服解密消息成功", zap.String("agent_id", c.AgentID), zap.String("chat_id", chatID))

		// 检查消息类型
		msgType, ok := decryptedMsgData["msgtype"].(string)
		if !ok {
			logger.Warn("客服消息类型字段缺失或格式错误，跳过", zap.String("agent_id", c.AgentID))
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
		case "voice":
			// 语音消息，下载语音文件并转文本
			voiceData, ok := decryptedMsgData["voice"].(map[string]interface{})
			if !ok {
				logger.Warn("客服语音消息格式错误，跳过", zap.String("agent_id", c.AgentID))
				continue
			}

			sdkFileid, ok := voiceData["sdkfileid"].(string)
			if !ok || sdkFileid == "" {
				logger.Warn("客服语音消息缺少 sdkfileid，跳过", zap.String("agent_id", c.AgentID))
				continue
			}

			// 获取消息中声明的语音文件大小（用于对比）
			var expectedSize uint32
			if voiceSize, ok := voiceData["voice_size"].(float64); ok {
				expectedSize = uint32(voiceSize)
			}

			// 下载语音文件
			voiceBytes, err := c.downloadVoiceFile(sdkFileid)
			if err != nil {
				logger.Error("客服下载语音文件失败", zap.String("agent_id", c.AgentID), zap.Error(err))
				continue
			}

			// 对比下载的语音文件大小
			actualSize := len(voiceBytes)
			if expectedSize > 0 {
				if uint32(actualSize) != expectedSize {
					logger.Warn("语音文件大小不匹配",
						zap.String("agent_id", c.AgentID),
						zap.Uint32("expected_size", expectedSize),
						zap.Int("actual_size", actualSize),
						zap.Int("difference", actualSize-int(expectedSize)))
				} else {
					logger.Debug("语音文件大小匹配",
						zap.String("agent_id", c.AgentID),
						zap.Int("size", actualSize))
				}
			} else {
				logger.Info("下载语音文件完成",
					zap.String("agent_id", c.AgentID),
					zap.Int("size", actualSize),
					zap.String("note", "消息中未提供预期大小"))
			}

			// 将语音转换为文本
			text, err := c.convertVoiceToText(voiceBytes)
			if err != nil {
				logger.Error("客服语音转文本失败", zap.String("agent_id", c.AgentID), zap.Error(err))
				// 即使转文本失败，也可以发送原始语音信息给 AI
				msgContent = []byte(fmt.Sprintf("[语音消息，转文本失败: %v]", err))
			} else {
				// 使用转换后的文本
				msgContent = []byte(text)
				logger.Info("客服语音转文本成功", zap.String("agent_id", c.AgentID), zap.String("text", text))
			}
		default:
			logger.Debug("客服收到不支持的消息类型，跳过", zap.String("agent_id", c.AgentID), zap.String("msg_type", msgType))
			continue
		}

		// 获取 msgid
		msgID := ""
		if id, ok := msgMap["msgid"].(string); ok {
			msgID = id
		}

		// 获取消息时间戳
		var msgTime time.Time
		if msgtime, ok := msgMap["msgtime"].(float64); ok {
			// msgtime 是毫秒时间戳
			msgTime = time.Unix(0, int64(msgtime)*int64(time.Millisecond))
		} else {
			msgTime = time.Now()
		}

		// 判断是否是客服发送的消息
		// 方法1: 通过 action 字段判断，action="send" 表示发送的消息
		// 方法2: 通过 from 字段判断，如果 from 等于 AgentID，则是客服发送的消息
		isAgentMessage := false
		if action, ok := decryptedMsgData["action"].(string); ok && action == "send" {
			// 如果 action 是 "send"，还需要确认是客服发送的
			// 检查 from 字段是否等于 AgentID
			if from, ok := decryptedMsgData["from"].(string); ok {
				if from == c.AgentID {
					isAgentMessage = true
				}
			}
		} else {
			// 如果没有 action 字段，通过 from 字段判断
			if from, ok := decryptedMsgData["from"].(string); ok {
				// 如果 from 字段的值是 AgentID，说明是客服发送的消息
				if from == c.AgentID {
					isAgentMessage = true
				}
			}
		}

		// 如果是客服发送的消息，异步处理 suggestion 关联
		if isAgentMessage && msgID != "" && len(msgContent) > 0 && db != nil {
			contentStr := string(msgContent)
			go c.linkSuggestionToMessage(c.AgentID, chatID, msgID, contentStr, msgTime)
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
				// 多条消息，用换行符拼接
				contents := make([]string, 0, len(msgs))
				for _, msg := range msgs {
					contents = append(contents, string(msg.Content))
				}
				aggregatedContent = []byte(strings.Join(contents, "\n"))
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

			logger.Info("客服发送聚合消息给 AI", zap.String("agent_id", c.AgentID), zap.String("chat_id", cid), zap.Int("message_count", len(msgs)))
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
