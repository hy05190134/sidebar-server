package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"

	"wework-sdk/wework"
)

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
