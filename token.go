package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"go.uber.org/zap"
)

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
		logger.Warn("access_token 未返回过期时间，使用默认值 7200 秒")
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

	logger.Info("access_token 已缓存", zap.Time("expire_at", expireAt), zap.Int("expires_in", expiresIn))

	return result.AccessToken, nil
}

// getJSAPITicket 获取企业微信 jsapi_ticket
// useAgentConfig: true 表示获取应用的 jsapi_ticket（用于 wx.agentConfig），使用 /cgi-bin/ticket/get?type=agent_config
//
//	false 表示获取企业的 jsapi_ticket（用于 wx.config），使用 /cgi-bin/get_jsapi_ticket
func getJSAPITicket(corpID, corpSecret string, useAgentConfig bool) (string, error) {
	// 根据类型选择缓存字段
	var cachedTicket *string
	var expireAt *time.Time

	if useAgentConfig {
		// 检查应用的 jsapi_ticket 缓存
		tokenCache.mu.RLock()
		if tokenCache.jsapiTicket != "" && time.Now().Before(tokenCache.ticketExpireAt) {
			ticket := tokenCache.jsapiTicket
			tokenCache.mu.RUnlock()
			return ticket, nil
		}
		tokenCache.mu.RUnlock()
		cachedTicket = &tokenCache.jsapiTicket
		expireAt = &tokenCache.ticketExpireAt
	} else {
		// 检查企业的 jsapi_ticket 缓存
		tokenCache.mu.RLock()
		if tokenCache.agentJSAPITicket != "" && time.Now().Before(tokenCache.agentTicketExpireAt) {
			ticket := tokenCache.agentJSAPITicket
			tokenCache.mu.RUnlock()
			return ticket, nil
		}
		tokenCache.mu.RUnlock()
		cachedTicket = &tokenCache.agentJSAPITicket
		expireAt = &tokenCache.agentTicketExpireAt
	}

	// 先获取 access_token
	accessToken, err := getAccessToken(corpID, corpSecret)
	if err != nil {
		return "", fmt.Errorf("获取 access_token 失败: %w", err)
	}

	// 根据类型选择不同的 API URL
	var apiURL string
	if useAgentConfig {
		// 应用的 jsapi_ticket API（使用企业的 ticket API）
		apiURL = fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/ticket/get?access_token=%s&type=agent_config", url.QueryEscape(accessToken))
	} else {
		// 企业的 jsapi_ticket API（使用应用的 ticket API）
		apiURL = fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/get_jsapi_ticket?access_token=%s", url.QueryEscape(accessToken))
	}

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
		ticketType := "企业"
		if useAgentConfig {
			ticketType = "应用"
		}
		return "", fmt.Errorf("获取%s jsapi_ticket 失败: errcode=%d, errmsg=%s", ticketType, result.ErrCode, result.ErrMsg)
	}

	// 使用微信服务端返回的实际过期时间
	expiresIn := result.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 7200
		ticketType := "企业"
		if useAgentConfig {
			ticketType = "应用"
		}
		logger.Warn(fmt.Sprintf("%s jsapi_ticket 未返回过期时间，使用默认值 7200 秒", ticketType))
	}

	// 提前10%的时间过期，但最少提前60秒
	earlyExpire := expiresIn / 10
	if earlyExpire < 60 {
		earlyExpire = 60
	}
	calculatedExpireAt := time.Now().Add(time.Duration(expiresIn-earlyExpire) * time.Second)

	tokenCache.mu.Lock()
	*cachedTicket = result.Ticket
	*expireAt = calculatedExpireAt
	tokenCache.mu.Unlock()

	ticketType := "企业"
	if useAgentConfig {
		ticketType = "应用"
	}
	logger.Info(fmt.Sprintf("%s jsapi_ticket 已缓存", ticketType), zap.Time("expire_at", calculatedExpireAt), zap.Int("expires_in", expiresIn))

	return result.Ticket, nil
}
