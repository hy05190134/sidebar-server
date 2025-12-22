package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"
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
