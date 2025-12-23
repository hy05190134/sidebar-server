# 企业微信侧边栏服务器 (Sidebar Server)

企业微信智能客服侧边栏服务器，提供实时消息轮询、AI 协助、语音转文本和智能建议关联等功能。

## 📋 目录

- [功能特性](#功能特性)
- [架构设计](#架构设计)
- [快速开始](#快速开始)
- [配置说明](#配置说明)
- [API 文档](#api-文档)
- [开发指南](#开发指南)

## ✨ 功能特性

### 核心功能

1. **WebSocket 实时通信**
   - 与企业微信侧边栏建立 WebSocket 连接
   - 支持多客服并发连接
   - 心跳保活机制

2. **会话存档轮询**
   - 定时轮询企业微信会话存档接口
   - 支持自定义轮询间隔（1秒 - 1小时）
   - 自动解密会话消息
   - 按 chatId 聚合消息

3. **消息类型支持**
   - 文本消息处理
   - 语音消息下载和转文本
   - 语音文件大小校验

4. **AI 协助功能**
   - 接收客户消息并触发 AI 分析
   - 返回 AI 建议给客服
   - 支持建议反馈（使用/编辑/拒绝）

5. **智能建议关联**
   - 自动匹配客服消息与 AI 建议
   - 余弦相似度计算
   - 相似度阈值过滤
   - 自动更新消息关联

6. **企业微信配置**
   - 自动获取和缓存 access_token
   - 自动获取和缓存 jsapi_ticket
   - 支持签名生成

## 🏗️ 架构设计

### 系统架构图

```
┌─────────────────────────────────────────────────────────────┐
│                     企业微信侧边栏 (前端)                      │
│                    (html/sidebar.html)                       │
└──────────────────────┬──────────────────────────────────────┘
                       │ WebSocket
                       │ HTTP API
┌──────────────────────▼──────────────────────────────────────┐
│                    Sidebar Server                            │
│  ┌──────────────────────────────────────────────────────┐  │
│  │              WebSocket Hub                           │  │
│  │  ┌──────────────┐  ┌──────────────┐  ┌───────────┐ │  │
│  │  │ WeComClient  │  │ WeComClient  │  │    ...    │ │  │
│  │  │ (Agent 1)    │  │ (Agent 2)    │  │           │ │  │
│  │  └──────┬───────┘  └──────┬───────┘  └───────────┘ │  │
│  └─────────┼─────────────────┼──────────────────────────┘  │
│            │                 │                              │
│  ┌─────────▼─────────────────▼──────────────────────────┐  │
│  │            Polling Service                            │  │
│  │  ┌────────────────────────────────────────────────┐   │  │
│  │  │  • 定时轮询会话存档                              │   │  │
│  │  │  • 消息解密                                      │   │  │
│  │  │  • 语音下载和转文本                              │   │  │
│  │  │  • 消息聚合                                      │   │  │
│  │  └────────────────────────────────────────────────┘   │  │
│  └───────────────────────────────────────────────────────┘  │
│                                                             │
│  ┌───────────────────────────────────────────────────────┐  │
│  │            AI Service                                 │  │
│  │  ┌────────────────────────────────────────────────┐   │  │
│  │  │  • AI 协助请求处理                               │   │  │
│  │  │  • AI 建议反馈                                   │   │  │
│  │  │  • 建议关联匹配                                   │   │  │
│  │  └────────────────────────────────────────────────┘   │  │
│  └───────────────────────────────────────────────────────┘  │
│                                                             │
│  ┌───────────────────────────────────────────────────────┐  │
│  │         Database Service (PostgreSQL)                 │  │
│  │  ┌────────────────────────────────────────────────┐   │  │
│  │  │  • Suggestion 存储                              │   │  │
│  │  │  • 相似度计算                                    │   │  │
│  │  │  • 消息关联更新                                  │   │  │
│  │  └────────────────────────────────────────────────┘   │  │
│  └───────────────────────────────────────────────────────┘  │
└──────────────────────┬──────────────────────────────────────┘
                       │
        ┌──────────────┼──────────────┐
        │              │              │
┌───────▼──────┐ ┌─────▼──────┐ ┌────▼──────┐
│ 企业微信 API  │ │ 语音识别 API │ │ PostgreSQL │
│              │ │            │ │           │
└──────────────┘ └────────────┘ └───────────┘
```

### 模块设计

#### 1. WebSocket Hub (`websocket.go`)

**职责：**
- 管理所有客服连接
- 处理连接注册和注销
- 消息广播

**核心组件：**
- `WeComHub`: Hub 管理器
- `WeComClient`: 单个客服连接客户端

**关键特性：**
- 并发安全的连接管理
- 心跳保活机制（30秒）
- 优雅的连接关闭

#### 2. 轮询服务 (`polling.go`)

**职责：**
- 定时轮询企业微信会话存档
- 消息解密和处理
- 语音消息处理
- 消息聚合和 AI 触发

**核心流程：**
```
启动轮询 → 初始化 SDK → 定时拉取消息 → 解密消息 → 
识别消息类型 → 处理消息内容 → 聚合消息 → 触发 AI 分析
```

**关键特性：**
- 可配置轮询间隔
- 支持动态调整轮询间隔
- 按 chatId 聚合消息
- 自动识别客服消息并关联 suggestion

#### 3. AI 服务 (`ai.go`)

**职责：**
- 处理 AI 协助请求
- 处理 AI 建议反馈
- 触发后续 AI 分析

**消息类型：**
- `ai_assistance_request`: AI 协助请求
- `ai_feedback`: AI 建议反馈
- `ai_suggestion`: AI 建议响应

#### 4. 数据库服务 (`database.go`)

**职责：**
- 数据库连接管理
- Suggestion 数据模型
- 相似度计算和匹配

**核心功能：**
- **余弦相似度计算**: 基于词频向量的文本相似度计算
- **智能匹配**: 支持精确匹配和相似度匹配
- **自动关联**: 自动将客服消息与 suggestion 关联

**匹配策略：**
1. 完全匹配 `original_content` → 相似率 100%
2. 完全匹配 `edited_content` → 计算与 `original_content` 的相似度
3. 相似度匹配 → 计算余弦相似度，超过阈值则关联

#### 5. 加密服务 (`crypto.go`)

**职责：**
- RSA 私钥解密
- 会话消息解密

**流程：**
```
encrypt_random_key → RSA 解密 → 获取解密密钥 → 
解密 encrypt_chat_msg → 获取明文消息
```

#### 6. Token 管理 (`token.go`)

**职责：**
- access_token 获取和缓存
- jsapi_ticket 获取和缓存
- 自动过期刷新

**缓存策略：**
- 提前 10% 时间过期（最少提前 60 秒）
- 线程安全的缓存访问

#### 7. 配置服务 (`config.go`)

**职责：**
- 企业微信配置生成
- JSAPI 签名生成

**签名算法：**
```
jsapi_ticket + noncestr + timestamp + url → 字典序排序 → 
SHA1 加密 → 签名
```

### 数据流设计

#### 消息处理流程

```
企业微信会话存档
    ↓
轮询服务拉取消息
    ↓
RSA 解密消息
    ↓
解析消息内容
    ↓
识别消息类型 (文本/语音)
    ↓
处理消息内容
    ├─ 文本消息 → 直接使用
    └─ 语音消息 → 下载 → 转文本
    ↓
按 chatId 聚合
    ↓
触发 AI 分析
    ↓
返回 AI 建议
    ↓
客服选择建议
    ↓
发送消息
    ↓
异步关联 suggestion
    ├─ 查询 suggestion 表
    ├─ 计算相似度
    └─ 更新关联关系
```

#### Suggestion 关联流程

```
客服发送消息
    ↓
提取消息内容
    ↓
查询 suggestion 表 (时间戳之前)
    ↓
计算相似度
    ├─ 完全匹配 original_content → 100%
    ├─ 完全匹配 edited_content → 计算相似度
    └─ 其他 → 余弦相似度计算
    ↓
筛选相似度 ≥ 阈值
    ↓
按相似度排序
    ↓
选择相似度最高的
    ↓
更新 msg_id 和 similarity
```

### 技术栈

- **语言**: Go 1.24+
- **WebSocket**: gorilla/websocket
- **数据库**: PostgreSQL + GORM
- **日志**: zap
- **配置**: godotenv
- **加密**: crypto/rsa, crypto/x509
- **HTTP**: net/http

## 🚀 快速开始

### 环境要求

- Go 1.24 或更高版本
- PostgreSQL 数据库
- 企业微信账号和会话存档权限
- RSA 私钥文件（从企业微信管理后台下载）

### 安装步骤

1. **克隆项目**
```bash
git clone <repository-url>
cd sidebar-server
```

2. **安装依赖**
```bash
go mod download
```

3. **配置环境变量**
```bash
cp env.example .env
# 编辑 .env 文件，填入实际配置
```

4. **初始化数据库**
```bash
# 确保 PostgreSQL 已启动
# 数据库会在首次启动时自动创建表结构
```

5. **运行服务**
```bash
go run .
# 或编译后运行
go build -o sidebar-server .
./sidebar-server
```

### Docker 部署

```bash
# 构建镜像
docker build -t sidebar-server .

# 运行容器
docker run -d \
  --name sidebar-server \
  -p 8080:8080 \
  --env-file .env \
  sidebar-server
```

## ⚙️ 配置说明

### 环境变量配置

详细配置说明请参考 `env.example` 文件。

#### 必需配置

- `WECOM_CORP_ID`: 企业微信企业 ID
- `WECOM_CORP_SECRET`: 企业微信应用密钥
- `WECOM_AGENT_ID`: 企业微信应用 ID
- `WECOM_RSA_PRIVATE_KEY_PATH`: RSA 私钥文件路径

#### 数据库配置

- `DB_HOST`: 数据库主机（默认: localhost）
- `DB_PORT`: 数据库端口（默认: 5432）
- `DB_USER`: 数据库用户（默认: postgres）
- `DB_PASSWORD`: 数据库密码
- `DB_NAME`: 数据库名称（默认: sidebar_db）
- `DB_SSLMODE`: SSL 模式（默认: disable）

#### 可选配置

- `WECOM_ARCHIVE_SECRET`: 会话存档专用 Secret
- `WECOM_PROXY`: 代理地址
- `WECOM_PROXY_PASSWD`: 代理密码
- `VOICE_RECOGNITION_API_URL`: 语音识别服务 URL
- `VOICE_RECOGNITION_API_KEY`: 语音识别服务 API Key
- `SUGGESTION_QUERY_LIMIT`: Suggestion 查询条数（默认: 10）
- `SUGGESTION_SIMILARITY_THRESHOLD`: 相似度阈值（默认: 80）

## 📡 API 文档

### WebSocket 端点

#### `/ws/wecom`

WebSocket 连接端点，用于与企业微信侧边栏通信。

**连接消息格式：**
```json
{
  "type": "auth",
  "agent_id": "agent_001",
  "chat_id": "chat_001"
}
```

**消息类型：**

1. **认证消息** (`auth`)
   - 建立连接时发送
   - 设置 `agent_id` 和 `chat_id`

2. **AI 协助请求** (`ai_assistance_request`)
   - 请求 AI 分析客户消息
   - 返回 AI 建议

3. **AI 反馈** (`ai_feedback`)
   - 反馈 AI 建议的使用情况
   - `action`: use/edit/reject

4. **设置轮询间隔** (`set_poll_interval`)
   - 动态调整轮询间隔
   - `interval`: 间隔时间（秒）

5. **获取轮询间隔** (`get_poll_interval`)
   - 查询当前轮询间隔

### HTTP 端点

#### `GET /api/wx-config`

获取企业微信 JSAPI 配置。

**查询参数：**
- `url` (可选): 当前页面 URL

**响应示例：**
```json
{
  "corpId": "wwd08c8exxxx5ab44d",
  "agentId": "1000001",
  "timestamp": 1603875609,
  "nonceStr": "abc123",
  "signature": "sha1_signature"
}
```

## 🔧 开发指南

### 项目结构

```
sidebar-server/
├── main.go              # 程序入口
├── types.go             # 类型定义
├── websocket.go         # WebSocket 服务
├── polling.go           # 轮询服务
├── ai.go                # AI 服务
├── database.go          # 数据库服务
├── crypto.go            # 加密服务
├── token.go             # Token 管理
├── config.go            # 配置服务
├── logger.go            # 日志初始化
├── go.mod               # Go 模块定义
├── go.sum               # 依赖校验
├── Makefile             # 构建脚本
├── env.example          # 环境变量示例
├── README.md            # 本文档
├── html/                # 前端 HTML
│   └── sidebar.html
├── js/                  # 前端 JavaScript
│   └── wecom-sidebar.js
└── go_sdk/              # 企业微信 SDK
    ├── wework/
    │   └── wework_sdk.go
    └── README.md
```

### 核心数据结构

#### WeComClient
```go
type WeComClient struct {
    Conn           *websocket.Conn
    AgentID        string
    ChatID         string
    Send           chan []byte
    weworkSDK      *wework.SDK
    pollSeq        uint64
    pollTicker     *time.Ticker
    pollStop       chan struct{}
    pollInterval   time.Duration
    pollIntervalCh chan time.Duration
}
```

#### Suggestion
```go
type Suggestion struct {
    ID              uint
    SuggestionID    string
    AgentID         string
    ChatID          string
    MsgID           string
    OriginalContent string
    EditedContent   string
    Text            string
    Confidence      float64
    Similarity      float64
    Action          string
    CreatedAt       time.Time
    UpdatedAt       time.Time
}
```

### 扩展开发

#### 添加新的消息类型

1. 在 `polling.go` 的 `switch msgType` 中添加新类型处理
2. 实现对应的消息处理逻辑
3. 更新前端 JavaScript 以支持新类型

#### 自定义相似度算法

修改 `database.go` 中的 `calculateCosineSimilarity` 函数，实现自定义相似度计算逻辑。

#### 添加新的 AI 服务

1. 在 `ai.go` 中添加新的处理函数
2. 在 `websocket.go` 的 `handleMessage` 中注册新消息类型
3. 更新前端以支持新的 AI 功能

### 测试

```bash
# 运行测试（如果有）
go test ./...

# 代码检查
go vet ./...

# 格式化代码
go fmt ./...
```

### 日志

项目使用 `zap` 进行结构化日志记录。

**日志级别：**
- `Info`: 一般信息
- `Debug`: 调试信息
- `Warn`: 警告信息
- `Error`: 错误信息
- `Fatal`: 致命错误（会退出程序）

**日志格式：**
```
时间戳 级别 消息 [字段1=值1] [字段2=值2] ...
```

## 📝 注意事项

1. **RSA 私钥安全**
   - 私钥文件需要妥善保管
   - 不要将私钥提交到代码仓库
   - 建议使用环境变量或密钥管理服务

2. **数据库连接**
   - 确保数据库连接池配置合理
   - 定期备份数据库
   - 监控数据库性能

3. **轮询间隔**
   - 建议轮询间隔不少于 1 秒
   - 过短的间隔可能导致 API 限流
   - 根据实际业务需求调整

4. **相似度阈值**
   - 默认阈值 80%，可根据实际情况调整
   - 阈值过低可能导致误匹配
   - 阈值过高可能导致漏匹配

5. **并发安全**
   - 所有共享资源都使用互斥锁保护
   - WebSocket 连接是线程安全的
   - 数据库操作使用 GORM 的事务支持

## 🤝 贡献指南

1. Fork 项目
2. 创建特性分支 (`git checkout -b feature/AmazingFeature`)
3. 提交更改 (`git commit -m 'Add some AmazingFeature'`)
4. 推送到分支 (`git push origin feature/AmazingFeature`)
5. 开启 Pull Request

## 📄 许可证

[添加许可证信息]

## 📞 联系方式

[添加联系方式]

---

**最后更新**: 2024年
