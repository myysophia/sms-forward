package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/joho/godotenv"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"time"
)

// SMS 短信数据结构
type SMS struct {
	From       string `json:"from" binding:"required"`
	Content    string `json:"content" binding:"required"`
	ReceivedAt int64  `json:"received_at,string" binding:"required"`
}

// Redis配置结构
type RedisConfig struct {
	Host     string
	Port     string
	Password string
	DB       int
	PoolSize int
}

// Redis客户端
var rdb *redis.Client

// 获取环境变量（带默认值）
func getEnvWithDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// 从环境变量加载Redis配置
func loadRedisConfig() *RedisConfig {
	// 加载.env文件
	_ = godotenv.Load()

	host := getEnvWithDefault("REDIS_HOST", "localhost")
	port := getEnvWithDefault("REDIS_PORT", "6379")
	password := getEnvWithDefault("REDIS_PASSWORD", "")
	db, err := strconv.Atoi(getEnvWithDefault("REDIS_DB", "0"))
	if err != nil {
		db = 0
	}
	poolSize, err := strconv.Atoi(getEnvWithDefault("REDIS_POOL_SIZE", "10"))
	if err != nil {
		poolSize = 10
	}
	return &RedisConfig{
		Host:     host,
		Port:     port,
		Password: password,
		DB:       db,
		PoolSize: poolSize,
	}
}

// 初始化Redis连接
func initRedis() {
	config := loadRedisConfig()
	addr := fmt.Sprintf("%s:%s", config.Host, config.Port)
	rdb = redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     config.Password,
		DB:           config.DB,
		PoolSize:     config.PoolSize,
		DialTimeout:  10 * time.Second,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		PoolTimeout:  30 * time.Second,
	})
	ctx := context.Background()
	pong, err := rdb.Ping(ctx).Result()
	if err != nil {
		log.Fatal("Redis连接失败:", err)
	}
	log.Printf("Redis连接成功: %s (地址: %s, 数据库: %d)", pong, addr, config.DB)
}

// 接收短信的API接口
// 用于提取 4–8 位连续数字（验证码）
var digitRe = regexp.MustCompile(`\d{4,8}`)

// extractDigits 找到第一串 4~8 位数字；找不到返回空字符串
func extractDigits(text string) string {
	return digitRe.FindString(text)
}

// POST /api/receive_sms
func receiveSMS(c *gin.Context) {
	var sms SMS

	// 1) 读取并打印原始请求体（便于调试）
	bodyBytes, err := c.GetRawData()
	if err != nil {
		log.Printf("读取请求体失败: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取请求体失败"})
		return
	}
	log.Printf("收到原始请求体: %s", string(bodyBytes))
	// 重新填充 Body，供 gin 解析
	c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	// 2) 解析 JSON（SMS.ReceivedAt 已加 `,string` 标签，兼容带引号时间戳）
	if err := c.ShouldBindJSON(&sms); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "参数错误",
			"message": err.Error(),
		})
		return
	}

	// 3) 提取验证码数字，只存入 Redis 纯数字
	code := extractDigits(sms.Content)
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "短信内容中未找到验证码数字",
			"message": "content 必须包含 4~8 位数字验证码",
		})
		return
	}
	sms.Content = code // 重写 content：仅保存数字

	// 4) 生成 Redis 键 & 序列化
	cacheKey := fmt.Sprintf("sms:%s:%d", sms.From, sms.ReceivedAt)
	smsData, err := json.Marshal(sms)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "数据序列化失败",
			"message": err.Error(),
		})
		return
	}

	// 5) 写 Redis（历史键 + 最新键）
	ctx := context.Background()
	if err := rdb.Set(ctx, cacheKey, smsData, 2*time.Minute).Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "缓存存储失败",
			"message": err.Error(),
		})
		return
	}
	latestKey := fmt.Sprintf("latest_sms:%s", sms.From)
	_ = rdb.Set(ctx, latestKey, smsData, 2*time.Minute).Err()

	// 6) 日志
	receivedAtTime := time.UnixMilli(sms.ReceivedAt)
	log.Printf("收到短信 - 来源: %s, 验证码: %s, 时间: %s",
		sms.From, sms.Content, receivedAtTime.Format("2006-01-02 15:04:05"))

	// 7) 响应
	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "短信接收成功",
		"data": gin.H{
			"cache_key": cacheKey,
			"from":      sms.From,
			"timestamp": sms.ReceivedAt,
			"code":      sms.Content, // 返回提取后的验证码
		},
	})
}

// 根据手机号获取最新短信
func getLatestSMS(c *gin.Context) {
	phone := c.Param("phone")
	if phone == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "手机号不能为空",
		})
		return
	}
	ctx := context.Background()
	latestKey := fmt.Sprintf("latest_sms:%s", phone)
	smsData, err := rdb.Get(ctx, latestKey).Result()
	if err == redis.Nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "未找到该手机号的短信记录",
		})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "查询失败",
			"message": err.Error(),
		})
		return
	}
	var sms SMS
	err = json.Unmarshal([]byte(smsData), &sms)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "数据解析失败",
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"data":   sms,
	})
}
func main() {
	// 初始化Redis
	initRedis()

	// 创建Gin路由
	r := gin.Default()
	r.Use(gin.Logger())
	r.Use(gin.Recovery())

	// 设置路由
	api := r.Group("/api")
	{
		api.POST("/receive_sms", receiveSMS)
		api.GET("/latest_sms/:phone", getLatestSMS)
	}

	// 启动服务
	port := getEnvWithDefault("SERVER_PORT", "8080")
	log.Printf("短信转发服务启动在端口 %s", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatal("服务启动失败:", err)
	}
}
