# 企业微信会话存档 Go SDK

这是企业微信会话存档 SDK 的 Go 语言版本，基于 C SDK 封装而成。

## 文件说明

- `wework_sdk.go`: Go SDK 主文件，包含所有 API 封装
- `example.go`: 使用示例代码
- `libWeWorkFinanceSdk_C.so`: C SDK 动态库文件（Linux 版本）
- `WeWorkFinanceSdk_C.h`: C SDK 头文件
- `config.txt`: 配置文件（可选），用于存储企业微信的 corpid 和 secret

## 目录结构

```
go_sdk/
├── libWeWorkFinanceSdk_C.so    # C SDK 动态库文件
├── WeWorkFinanceSdk_C.h         # C SDK 头文件
├── wework/                      # Go SDK 包目录
│   ├── wework_sdk.go           # Go SDK 主文件
│   └── WeWorkFinanceSdk_C.h    # 头文件副本
├── example.go                   # 使用示例
├── config.txt                   # 配置文件（可选）
├── Dockerfile                   # Docker 编译配置
├── build.sh                     # 编译脚本
└── README_GO.md                 # 本文档
```

## 编译要求

1. Go 1.13 或更高版本
2. GCC 编译器（用于 cgo）
3. 确保 `libWeWorkFinanceSdk_C.so` 在项目根目录中

## 编译示例程序

### Linux 环境

```bash
# 方式1: 直接编译
go build -o example example.go

# 方式2: 直接运行
go run example.go [参数]

# 如果遇到链接错误，可以设置库路径
export LD_LIBRARY_PATH=$(pwd):$LD_LIBRARY_PATH
./example [参数]
```

### macOS 环境

由于 SDK 库文件是 Linux 版本（`.so` 格式），在 macOS 上需要使用 Docker 编译：

```bash
# 使用 Docker 编译 Linux 版本
./build.sh
```

编译出的二进制文件只能在 Linux 环境中运行。

**注意**：如果需要在 macOS 上直接运行，需要获取 macOS 版本的 SDK 库文件（`.dylib` 格式）。

## 配置文件

程序会尝试从 `config.txt` 文件读取企业微信的 `corpid` 和 `secret`。如果文件不存在，会使用代码中的默认值。

创建 `config.txt` 文件格式：
```
wwd08c8exxxx5ab44d
your_secret_here
```

- 第一行：企业微信的企业 ID (corpid)
- 第二行：会话内容存档的 Secret

## example 程序使用说明

### 查看帮助

直接运行程序（不带参数）会显示使用说明：

```bash
./example
```

### 1. 获取会话存档

```bash
./example 1 seq limit proxy passwd timeout
```

参数说明：
- `seq`: 从指定的 seq 开始拉取消息，首次使用请使用 `0`
- `limit`: 一次拉取的消息数量，最大值 1000
- `proxy`: 代理地址，不需要代理时传空字符串 `""`
- `passwd`: 代理账号密码，不需要代理时传空字符串 `""`
- `timeout`: 超时时间，单位秒

示例：
```bash
# 首次获取，拉取 100 条消息，不使用代理，超时 30 秒
./example 1 0 100 "" "" 30

# 使用代理
./example 1 0 100 "socks5://10.0.0.1:8081" "user:pass" 30
```

### 2. 获取媒体文件

```bash
./example 2 fileid proxy passwd timeout savefile
```

参数说明：
- `fileid`: 从 GetChatData 返回的会话消息中的 `sdkfileid`
- `proxy`: 代理地址，不需要代理时传空字符串 `""`
- `passwd`: 代理账号密码，不需要代理时传空字符串 `""`
- `timeout`: 超时时间，单位秒
- `savefile`: 保存文件的路径

示例：
```bash
# 下载媒体文件到 media.dat
./example 2 "CAQQ2fbb4QUY0On2rYSAgAMgip/yzgs=" "" "" 30 "media.dat"
```

### 3. 解密会话存档数据

```bash
./example 3 encrypt_key encrypt_chat_msg
```

参数说明：
- `encrypt_key`: 从 GetChatData 返回的 `encrypt_random_key`，需要先用企业自己的 RSA 私钥解密
- `encrypt_chat_msg`: 从 GetChatData 返回的 `encrypt_chat_msg`

示例：
```bash
./example 3 "decrypted_encrypt_key" "encrypt_chat_msg_content"
```

## 使用方法（编程方式）

### 1. 初始化 SDK

```go
package main

import (
    "fmt"
    "wework-sdk/wework"
)

func main() {
    // 创建 SDK 实例
    sdk := wework.NewSDK()
    defer sdk.Destroy()

    // 初始化 SDK
    err := sdk.Init("your_corpid", "your_secret")
    if err != nil {
        fmt.Printf("初始化失败: %v\n", err)
        return
    }
}
```

### 2. 获取会话存档

```go
// seq: 从指定的seq开始拉取消息，首次使用请使用seq:0
// limit: 一次拉取的消息数量，最大值1000
// proxy: 代理地址，不需要代理时传空字符串
// passwd: 代理账号密码，不需要代理时传空字符串
// timeout: 超时时间，单位秒
chatData, err := sdk.GetChatData(0, 100, "", "", 30)
if err != nil {
    fmt.Printf("获取会话存档失败: %v\n", err)
    return
}

fmt.Printf("会话存档数据: %s\n", chatData.Data)
```

### 3. 获取媒体文件

```go
// 媒体文件需要分片下载，每次下载 512k
indexbuf := ""
isFinish := false
file, _ := os.OpenFile("media_file.dat", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
defer file.Close()

for !isFinish {
    mediaData, err := sdk.GetMediaData(indexbuf, sdkFileid, "", "", 30)
    if err != nil {
        fmt.Printf("获取媒体数据失败: %v\n", err)
        break
    }

    // 写入文件
    file.Write(mediaData.Data)

    indexbuf = mediaData.OutIndex
    isFinish = mediaData.IsFinish
}
```

### 4. 解密会话存档数据

```go
// encryptKey: 从 GetChatData 返回的 encrypt_random_key，使用企业自己的 RSA 私钥解密后得到
// encryptMsg: 从 GetChatData 返回的 encrypt_chat_msg
decryptedMsg, err := wework.DecryptData(encryptKey, encryptMsg)
if err != nil {
    fmt.Printf("解密失败: %v\n", err)
    return
}

fmt.Printf("解密后的消息: %s\n", decryptedMsg)
```

## API 说明

### SDK 结构体

```go
type SDK struct {
    // SDK 实例（内部使用）
}
```

### ChatData 结构体

```go
type ChatData struct {
    Data string  // 会话存档数据（JSON 格式）
    Len  int     // 数据长度
}
```

### MediaData 结构体

```go
type MediaData struct {
    Data     []byte  // 媒体文件数据
    OutIndex string  // 下次获取需要的索引
    IsFinish bool    // 是否下载完成
    DataLen  int     // 数据长度
    IndexLen int     // 索引长度
}
```

### 主要方法

#### NewSDK() *SDK
创建新的 SDK 实例。

#### (s *SDK) Init(corpid, secret string) error
初始化 SDK。
- `corpid`: 企业微信的企业id
- `secret`: 会话内容存档的Secret

#### (s *SDK) GetChatData(seq uint64, limit uint32, proxy, passwd string, timeout int) (*ChatData, error)
获取会话存档数据。
- `seq`: 从指定的seq开始拉取消息，首次使用请使用seq:0
- `limit`: 一次拉取的消息数量，最大值1000
- `proxy`: 代理地址，不需要代理时传空字符串
- `passwd`: 代理账号密码，不需要代理时传空字符串
- `timeout`: 超时时间，单位秒

#### (s *SDK) GetMediaData(indexbuf, sdkFileid, proxy, passwd string, timeout int) (*MediaData, error)
获取媒体文件数据。
- `indexbuf`: 分片索引，首次调用传空字符串
- `sdkFileid`: 从GetChatData返回的会话消息中的sdkfileid
- `proxy`: 代理地址，不需要代理时传空字符串
- `passwd`: 代理账号密码，不需要代理时传空字符串
- `timeout`: 超时时间，单位秒

#### DecryptData(encryptKey, encryptMsg string) (string, error)
解密会话存档数据。
- `encryptKey`: 解密后的encrypt_random_key
- `encryptMsg`: encrypt_chat_msg

#### (s *SDK) Destroy()
销毁 SDK 实例，释放资源。

## 错误码

| 错误码 | 说明 |
|--------|------|
| 10000 | 参数错误 |
| 10001 | 密钥错误 |
| 10002 | 数据解密失败 |
| 10003 | 系统失败 |
| 10004 | 密钥解密失败 |
| 10005 | fileid错误 |
| 10006 | 解密失败 |
| 10007 | 找不到信息加密版本对应的私钥，需要重新下载私钥 |
| 10008 | 解密encrypt_key失败 |
| 10009 | ip白名单 |
| 10010 | 数据过期 |
| 10011 | 证书错误 |

## 注意事项

1. **SDK 实例是线程不安全的**，每个 goroutine 应该创建独立的 SDK 实例
2. **使用完 SDK 后必须调用 `Destroy()` 方法**释放资源
3. **媒体文件下载需要分片进行**，每次下载 512k，直到 `IsFinish` 为 `true`
4. **首次获取会话存档时**，`seq` 参数应设置为 0
5. **解密数据需要使用企业自己的 RSA 私钥**先解密 `encrypt_random_key`，然后再调用 `DecryptData`
6. **首次运行前**：建议创建 `config.txt` 文件，填入正确的 `corpid` 和 `secret`

## 运行环境

- **Linux 环境**：可以直接运行编译好的 `example` 二进制文件
- **macOS 环境**：需要使用 Docker 编译 Linux 版本，然后在 Linux 环境中运行（或获取 macOS 版本的 SDK 库文件）
- **Windows 环境**：需要获取 Windows 版本的 SDK 库文件

## 许可证

版权所有，保留所有权利。
