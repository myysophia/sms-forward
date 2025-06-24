# SMS Forward Service

一个轻量级的短信转发服务，用于接收、处理和存储短信验证码。该服务提供 RESTful API 接口，支持短信接收和查询功能，并使用 Redis 进行数据缓存。
<img width="1077" alt="image" src="https://github.com/user-attachments/assets/012ba785-8f5a-4850-968d-d7ef1287f500" />


## 功能特点

- 接收短信并自动提取验证码（4-8位数字）
- 支持通过手机号查询最新短信
- 使用 Redis 进行数据缓存，支持数据过期
- 提供 Docker 支持，便于部署
- 支持环境变量配置

## 技术栈

- Go 1.22+
- Gin Web 框架
- Redis 缓存
- Docker 容器化

## 快速开始

### 使用 Docker 运行

1. 克隆仓库：
```bash
git clone <repository-url>
cd sms-forward
```

2. 配置环境变量（可选）：
创建 `.env` 文件并设置以下变量：
```env
SERVER_PORT=8080
REDIS_HOST=redis
REDIS_PORT=6379
REDIS_PASSWORD=your_password
REDIS_DB=0
REDIS_POOL_SIZE=10
```

3. 使用 Docker Compose 启动服务：
```bash
docker-compose up -d
```

### 手动运行

1. 确保已安装 Go 1.22+ 和 Redis

2. 安装依赖：
```bash
go mod download
```

3. 运行服务：
```bash
go run main.go
```

## API 接口

### 1. 接收短信

- **URL**: `/api/receive_sms`
- **方法**: POST
- **请求体**:
```json
{
    "from": "13800138000",
    "content": "您的验证码是：123456，5分钟内有效",
    "received_at": "1648888888888"
}
```
- **响应**:
```json
{
    "status": "success",
    "message": "短信接收成功",
    "data": {
        "cache_key": "sms:13800138000:1648888888888",
        "from": "13800138000",
        "timestamp": 1648888888888,
        "code": "123456"
    }
}
```

### 2. 查询最新短信

- **URL**: `/api/latest_sms/:phone`
- **方法**: GET
- **参数**: phone - 手机号码
- **响应**:
```json
{
    "status": "success",
    "data": {
        "from": "13800138000",
        "content": "123456",
        "received_at": 1648888888888
    }
}
```

## 配置说明

服务支持以下环境变量配置：

| 变量名 | 说明 | 默认值 |
|--------|------|--------|
| SERVER_PORT | 服务端口 | 8080 |
| REDIS_HOST | Redis 主机地址 | localhost |
| REDIS_PORT | Redis 端口 | 6379 |
| REDIS_PASSWORD | Redis 密码 | "" |
| REDIS_DB | Redis 数据库索引 | 0 |
| REDIS_POOL_SIZE | Redis 连接池大小 | 10 |

## 开发说明

### 项目结构

```
sms-forward/
├── main.go          # 主程序入口
├── Dockerfile       # Docker 构建文件
├── go.mod          # Go 模块定义
├── go.sum          # Go 依赖校验
└── .env            # 环境变量配置（可选）
```

### 构建 Docker 镜像

```bash
docker build -t sms-forward .
```

## 注意事项

1. 短信验证码在 Redis 中的存储时间为 2 分钟
2. 建议在生产环境中通过环境变量注入 Redis 密码
3. 服务默认使用非 root 用户运行，提高安全性

## License

[添加许可证信息]
# sms-forward
# sms-forward
