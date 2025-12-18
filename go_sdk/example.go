package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"wework-sdk/wework"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("用法:")
		fmt.Println("  ./example 1 seq limit proxy passwd timeout          # 获取会话存档")
		fmt.Println("  ./example 2 fileid proxy passwd timeout savefile   # 获取媒体文件")
		fmt.Println("  ./example 3 encrypt_key encrypt_chat_msg           # 解密会话存档数据")
		os.Exit(1)
	}

	// 创建 SDK 实例
	sdk := wework.NewSDK()
	defer sdk.Destroy()

	// 从配置文件读取 corpid 和 secret
	corpid := "ww*************"
	secret := "y**********************4"

	if data, err := os.ReadFile("config.txt"); err == nil {
		lines := strings.TrimSpace(string(data))
		parts := strings.Split(lines, "\n")
		if len(parts) >= 2 {
			corpid = strings.TrimSpace(parts[0])
			secret = strings.TrimSpace(parts[1])
		}
	}

	// 初始化 SDK
	if err := sdk.Init(corpid, secret); err != nil {
		fmt.Printf("初始化 SDK 失败: %v\n", err)
		os.Exit(1)
	}

	cmdType, _ := strconv.Atoi(os.Args[1])

	switch cmdType {
	case 1:
		// 获取会话存档
		seq, _ := strconv.ParseUint(os.Args[2], 10, 64)
		limit, _ := strconv.ParseUint(os.Args[3], 10, 32)
		proxy := os.Args[4]
		passwd := os.Args[5]
		timeout, _ := strconv.Atoi(os.Args[6])

		chatData, err := sdk.GetChatData(seq, uint32(limit), proxy, passwd, timeout)
		if err != nil {
			fmt.Printf("获取会话存档失败: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("获取会话存档成功，长度: %d\n", chatData.Len)
		fmt.Printf("数据: %s\n", chatData.Data)

		// 解析 JSON 数据示例
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(chatData.Data), &result); err == nil {
			fmt.Printf("解析后的 JSON: %+v\n", result)
		}

	case 2:
		// 获取媒体文件
		sdkFileid := os.Args[2]
		proxy := os.Args[3]
		passwd := os.Args[4]
		timeout, _ := strconv.Atoi(os.Args[5])
		saveFile := os.Args[6]

		indexbuf := ""
		isFinish := false

		// 打开文件用于追加写入
		file, err := os.OpenFile(saveFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			fmt.Printf("打开文件失败: %v\n", err)
			os.Exit(1)
		}
		defer file.Close()

		// 分片下载媒体文件
		for !isFinish {
			fmt.Printf("当前索引: %s\n", indexbuf)

			mediaData, err := sdk.GetMediaData(indexbuf, sdkFileid, proxy, passwd, timeout)
			if err != nil {
				fmt.Printf("获取媒体数据失败: %v\n", err)
				os.Exit(1)
			}

			fmt.Printf("数据大小: %d, 是否完成: %v, 下次索引: %s\n",
				mediaData.DataLen, mediaData.IsFinish, mediaData.OutIndex)

			// 写入文件
			if _, err := file.Write(mediaData.Data); err != nil {
				fmt.Printf("写入文件失败: %v\n", err)
				os.Exit(1)
			}

			indexbuf = mediaData.OutIndex
			isFinish = mediaData.IsFinish
		}

		fmt.Printf("媒体文件下载完成，保存到: %s\n", saveFile)

	case 3:
		// 解密会话存档数据
		encryptKey := os.Args[2]
		encryptMsg := os.Args[3]

		decryptedMsg, err := wework.DecryptData(encryptKey, encryptMsg)
		if err != nil {
			fmt.Printf("解密失败: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("解密后的消息: %s\n", decryptedMsg)

	default:
		fmt.Println("未知的命令类型")
		os.Exit(1)
	}
}
