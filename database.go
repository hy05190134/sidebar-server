package main

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormLogger "gorm.io/gorm/logger"
)

var db *gorm.DB

// Suggestion suggestion 表模型
type Suggestion struct {
	ID              uint    `gorm:"primaryKey;autoIncrement"`
	SuggestionID    string  `gorm:"type:varchar(255);uniqueIndex;not null"`
	AgentID         string  `gorm:"type:varchar(255);index;not null"`
	ChatID          string  `gorm:"type:varchar(255);index"`
	MsgID           string  `gorm:"type:varchar(255);index"` // 关联的消息ID
	OriginalContent string  `gorm:"type:text"`
	EditedContent   string  `gorm:"type:text"`
	Text            string  `gorm:"type:text"`
	Confidence      float64 `gorm:"type:decimal(5,2)"`
	Similarity      float64 `gorm:"type:decimal(5,2);default:0"` // 相似率（0-100）
	Action          string  `gorm:"type:varchar(50)"`            // use, edit, reject
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// MatchedSuggestion 匹配的 suggestion 结果，包含相似度信息
type MatchedSuggestion struct {
	Suggestion
	Similarity float64 // 相似度（0-100）
	MatchType  string  // 匹配类型：exact_original, exact_edited, similar
}

// TableName 指定表名
func (Suggestion) TableName() string {
	return "suggestions"
}

// initDatabase 初始化数据库连接
func initDatabase() error {
	// 从环境变量获取数据库连接信息
	host := os.Getenv("DB_HOST")
	if host == "" {
		host = "localhost"
	}

	port := os.Getenv("DB_PORT")
	if port == "" {
		port = "5432"
	}

	user := os.Getenv("DB_USER")
	if user == "" {
		user = "postgres"
	}

	password := os.Getenv("DB_PASSWORD")
	if password == "" {
		password = ""
	}

	dbname := os.Getenv("DB_NAME")
	if dbname == "" {
		dbname = "sidebar_db"
	}

	sslmode := os.Getenv("DB_SSLMODE")
	if sslmode == "" {
		sslmode = "disable"
	}

	// 构建 DSN
	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		host, port, user, password, dbname, sslmode)

	// 配置 GORM logger
	gormLog := gormLogger.New(
		&zapLoggerAdapter{},
		gormLogger.Config{
			SlowThreshold:             time.Second,
			LogLevel:                  gormLogger.Info,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		},
	)

	// 连接数据库
	var err error
	db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: gormLog,
	})
	if err != nil {
		return fmt.Errorf("连接数据库失败: %w", err)
	}

	// 自动迁移表结构
	if err := db.AutoMigrate(&Suggestion{}); err != nil {
		return fmt.Errorf("数据库迁移失败: %w", err)
	}

	logger.Info("数据库连接成功")
	return nil
}

// zapLoggerAdapter 适配 zap logger 到 GORM logger
type zapLoggerAdapter struct{}

func (z *zapLoggerAdapter) Printf(format string, v ...interface{}) {
	logger.Info(fmt.Sprintf(format, v...))
}

// calculateCosineSimilarity 计算两个文本的余弦相似度（返回 0-100 的百分比）
func calculateCosineSimilarity(text1, text2 string) float64 {
	if text1 == "" || text2 == "" {
		return 0.0
	}

	// 如果完全相同，返回 100%
	if text1 == text2 {
		return 100.0
	}

	// 分词并构建词频向量
	words1 := tokenize(text1)
	words2 := tokenize(text2)

	// 获取所有唯一的词
	allWords := make(map[string]bool)
	for word := range words1 {
		allWords[word] = true
	}
	for word := range words2 {
		allWords[word] = true
	}

	// 构建向量
	vector1 := make([]float64, 0, len(allWords))
	vector2 := make([]float64, 0, len(allWords))

	for word := range allWords {
		vector1 = append(vector1, float64(words1[word]))
		vector2 = append(vector2, float64(words2[word]))
	}

	// 计算余弦相似度
	dotProduct := 0.0
	magnitude1 := 0.0
	magnitude2 := 0.0

	for i := 0; i < len(vector1); i++ {
		dotProduct += vector1[i] * vector2[i]
		magnitude1 += vector1[i] * vector1[i]
		magnitude2 += vector2[i] * vector2[i]
	}

	if magnitude1 == 0 || magnitude2 == 0 {
		return 0.0
	}

	similarity := dotProduct / (math.Sqrt(magnitude1) * math.Sqrt(magnitude2))
	// 转换为百分比
	return similarity * 100.0
}

// tokenize 将文本分词并返回词频映射
func tokenize(text string) map[string]int {
	words := make(map[string]int)

	// 简单的分词：按空格和标点符号分割
	runes := []rune(text)
	currentWord := strings.Builder{}

	for _, r := range runes {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			currentWord.WriteRune(unicode.ToLower(r))
		} else {
			if currentWord.Len() > 0 {
				word := currentWord.String()
				if len(word) > 1 { // 忽略单字符
					words[word]++
				}
				currentWord.Reset()
			}
		}
	}

	// 处理最后一个词
	if currentWord.Len() > 0 {
		word := currentWord.String()
		if len(word) > 1 {
			words[word]++
		}
	}

	return words
}

// findSuggestionsByContent 根据内容查询 suggestion 记录并计算相似度
// 查询时间戳在指定时间之前的 n 条记录，计算与 original_content 或 edited_content 的相似度
func findSuggestionsByContent(agentID string, chatID string, content string, beforeTime time.Time, limit int) ([]MatchedSuggestion, error) {
	var suggestions []Suggestion

	// 查询所有符合条件的记录（不限制内容匹配）
	query := db.Where("agent_id = ? AND chat_id = ? AND created_at < ?", agentID, chatID, beforeTime).
		Order("created_at DESC").
		Limit(limit * 2) // 多查询一些，用于相似度筛选

	if err := query.Find(&suggestions).Error; err != nil {
		return nil, fmt.Errorf("查询 suggestion 失败: %w", err)
	}

	// 获取相似度阈值（默认 80%）
	similarityThreshold := 80.0
	if thresholdStr := os.Getenv("SUGGESTION_SIMILARITY_THRESHOLD"); thresholdStr != "" {
		if threshold, err := fmt.Sscanf(thresholdStr, "%f", &similarityThreshold); err != nil || threshold != 1 {
			similarityThreshold = 80.0
		}
	}

	var matchedSuggestions []MatchedSuggestion

	for _, sug := range suggestions {
		var similarity float64
		var matchType string

		// 检查是否与 original_content 完全相同
		if sug.OriginalContent == content {
			similarity = 100.0
			matchType = "exact_original"
		} else if sug.EditedContent == content {
			// 如果与 edited_content 完全相同，计算 edited_content 与 original_content 的相似度
			if sug.OriginalContent != "" {
				similarity = calculateCosineSimilarity(sug.EditedContent, sug.OriginalContent)
			} else {
				similarity = 100.0 // 如果没有 original_content，认为相似度为 100%
			}
			matchType = "exact_edited"
		} else {
			// 计算与 original_content 的余弦相似度
			if sug.OriginalContent != "" {
				similarity = calculateCosineSimilarity(content, sug.OriginalContent)
				matchType = "similar"
			} else {
				continue // 如果没有 original_content，跳过
			}
		}

		// 只返回相似度达到阈值的记录
		if similarity >= similarityThreshold {
			matchedSuggestions = append(matchedSuggestions, MatchedSuggestion{
				Suggestion: sug,
				Similarity: similarity,
				MatchType:  matchType,
			})
		}
	}

	// 按相似度从高到低排序，相似度相同时按创建时间倒序（最新的优先）
	sort.Slice(matchedSuggestions, func(i, j int) bool {
		if matchedSuggestions[i].Similarity != matchedSuggestions[j].Similarity {
			return matchedSuggestions[i].Similarity > matchedSuggestions[j].Similarity
		}
		// 相似度相同时，按创建时间倒序
		return matchedSuggestions[i].CreatedAt.After(matchedSuggestions[j].CreatedAt)
	})

	// 限制返回数量
	if len(matchedSuggestions) > limit {
		matchedSuggestions = matchedSuggestions[:limit]
	}

	return matchedSuggestions, nil
}

// updateSuggestionMsgID 更新 suggestion 的 msg_id 和相似率
func updateSuggestionMsgID(suggestionID string, msgID string, similarity float64) error {
	result := db.Model(&Suggestion{}).
		Where("suggestion_id = ?", suggestionID).
		Updates(map[string]interface{}{
			"msg_id":     msgID,
			"similarity": similarity,
		})

	if result.Error != nil {
		return fmt.Errorf("更新 suggestion msg_id 和相似率失败: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("未找到 suggestion_id: %s", suggestionID)
	}

	return nil
}
