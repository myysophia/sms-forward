package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/joho/godotenv"
)

/* ---------- 数据结构 ---------- */

// SMS 短信数据结构
type SMS struct {
	From       string `json:"from" binding:"required"`
	Content    string `json:"content" binding:"required"`
	ReceivedAt int64  `json:"received_at,string" binding:"required"` // 兼容带引号时间戳
}

// QueryRequest 查询请求数据结构
type QueryRequest struct {
	Phone string `json:"phone" binding:"required"`
}

// Redis配置结构
type RedisConfig struct {
	Host     string
	Port     string
	Password string
	DB       int
	PoolSize int
}

/* ---------- 全局变量 ---------- */

var (
	rdb *redis.Client

	// 提取验证码：优先匹配“验证码…123456”，否则取最后一串 4~8 位数字
	reCodeSpecific = regexp.MustCompile(`验证码[^0-9]*([0-9]{4,8})`)
	reCodeFallback = regexp.MustCompile(`[0-9]{4,8}`)
)

/* ---------- 工具函数 ---------- */

// 获取环境变量（带默认值）
func getEnvWithDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

// 从.env / 环境变量加载 Redis 配置
func loadRedisConfig() *RedisConfig {
	_ = godotenv.Load()

	db, _ := strconv.Atoi(getEnvWithDefault("REDIS_DB", "0"))
	pool, _ := strconv.Atoi(getEnvWithDefault("REDIS_POOL_SIZE", "10"))

	return &RedisConfig{
		Host:     getEnvWithDefault("REDIS_HOST", "localhost"),
		Port:     getEnvWithDefault("REDIS_PORT", "6379"),
		Password: getEnvWithDefault("REDIS_PASSWORD", ""),
		DB:       db,
		PoolSize: pool,
	}
}

// 初始化 Redis 连接
func initRedis() {
	cfg := loadRedisConfig()
	addr := fmt.Sprintf("%s:%s", cfg.Host, cfg.Port)

	rdb = redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     cfg.PoolSize,
		DialTimeout:  10 * time.Second,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		PoolTimeout:  30 * time.Second,
	})

	if pong, err := rdb.Ping(context.Background()).Result(); err != nil {
		log.Fatalf("Redis连接失败: %v", err)
	} else {
		log.Printf("Redis连接成功: %s (地址: %s, DB: %d)", pong, addr, cfg.DB)
	}
}

// extractCode 提取 4–8 位数字验证码
func extractCode(text string) string {
	if m := reCodeSpecific.FindStringSubmatch(text); len(m) == 2 {
		return m[1] // 「验证码 … 123456」
	}
	// fallback：取最后一串数字
	nums := reCodeFallback.FindAllString(text, -1)
	if len(nums) > 0 {
		return nums[len(nums)-1]
	}
	return ""
}

/* ---------- 路由处理 ---------- */

// POST /api/receive_sms
func receiveSMS(c *gin.Context) {
	var sms SMS

	// 1) 读取并打印原始请求体
	bodyBytes, err := c.GetRawData()
	if err != nil {
		log.Printf("读取请求体失败: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取请求体失败"})
		return
	}
	log.Printf("收到原始请求体: %s", string(bodyBytes))
	c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	// 2) 解析 JSON
	if err := c.ShouldBindJSON(&sms); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误", "message": err.Error()})
		return
	}

	// 3) 提取验证码
	code := extractCode(sms.Content)
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "未找到验证码数字"})
		return
	}
	sms.Content = code // 仅保存数字验证码

	// 4) 序列化并写 Redis
	keyHistoric := fmt.Sprintf("sms:%s:%d", sms.From, sms.ReceivedAt)
	data, _ := json.Marshal(sms)

	ctx := context.Background()
	if err := rdb.Set(ctx, keyHistoric, data, 2*time.Minute).Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "缓存存储失败", "message": err.Error()})
		return
	}
	_ = rdb.Set(ctx, fmt.Sprintf("latest_sms:%s", sms.From), data, 2*time.Minute).Err()

	// 5) 日志
	log.Printf("收到短信 - 来源:%s 验证码:%s 时间:%s",
		sms.From, sms.Content, time.UnixMilli(sms.ReceivedAt).Format("2006-01-02 15:04:05"))

	// 6) 响应
	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"data": gin.H{
			"cache_key": keyHistoric,
			"from":      sms.From,
			"timestamp": sms.ReceivedAt,
			"code":      sms.Content,
		},
	})
}

// GET /api/latest_sms/:phone
func getLatestSMS(c *gin.Context) {
	phone := c.Param("phone")
	log.Printf("接收到查询请求，phone参数: %s", phone)
	log.Printf("phone参数长度: %d", len(phone))

	if phone == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "手机号不能为空"})
		return
	}

	ctx := context.Background()
	redisKey := fmt.Sprintf("latest_sms:%s", phone)
	log.Printf("查询Redis key: %s", redisKey)

	data, err := rdb.Get(ctx, redisKey).Result()
	if err == redis.Nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "未找到该手机号的短信记录"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败", "message": err.Error()})
		return
	}

	var sms SMS
	if err := json.Unmarshal([]byte(data), &sms); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "数据解析失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "data": sms})
}

// POST /api/query_sms
func querySMS(c *gin.Context) {
	var req QueryRequest

	// 1) 读取并打印原始请求体
	bodyBytes, err := c.GetRawData()
	if err != nil {
		log.Printf("读取请求体失败: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取请求体失败"})
		return
	}
	log.Printf("收到查询请求体: %s", string(bodyBytes))
	c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	// 2) 解析 JSON
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误", "message": err.Error()})
		return
	}

	log.Printf("查询手机号: %s", req.Phone)

	if req.Phone == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "手机号不能为空"})
		return
	}

	ctx := context.Background()
	redisKey := fmt.Sprintf("latest_sms:%s", req.Phone)
	log.Printf("查询Redis key: %s", redisKey)

	data, err := rdb.Get(ctx, redisKey).Result()
	if err == redis.Nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "未找到该手机号的短信记录"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败", "message": err.Error()})
		return
	}

	var sms SMS
	if err := json.Unmarshal([]byte(data), &sms); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "数据解析失败"})
		return
	}

	log.Printf("查询成功 - 来源:%s 验证码:%s", sms.From, sms.Content)
	c.JSON(http.StatusOK, gin.H{"status": "success", "data": sms})
}

/* ---------- 启动入口 ---------- */

func main() {
	initRedis()

	r := gin.Default()
	r.Use(gin.Logger(), gin.Recovery())

	api := r.Group("/api")
	{
		api.POST("/receive_sms", receiveSMS)
		api.GET("/latest_sms/:phone", getLatestSMS)
		api.POST("/query_sms", querySMS) // 新增POST查询接口
	}

	port := getEnvWithDefault("SERVER_PORT", "8080")
	log.Printf("短信转发服务启动在端口 %s", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}
