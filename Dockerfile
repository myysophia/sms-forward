# syntax=docker/dockerfile:1
#
# 多阶段构建：Builder → Runtime
#    - Builder：用 Go 官方镜像编译（GOTOOLCHAIN=auto 会自动下载 go 1.24.1）
#    - Runtime：极简 Alpine 只带根证书，最终镜像 ≈15 MB

########################
# 1️⃣  Build stage
########################
FROM golang:1.22-alpine AS builder

# 让 Go 自动拉符合 go.mod 版本的编译器（≥1.24.1）
ENV GOTOOLCHAIN=auto

WORKDIR /app
RUN apk add --no-cache git tzdata  # git：拉私有包时用；tzdata：日志按本地时区

# 利用缓存：先复制 go.{mod,sum} 并下载依赖
COPY go.mod go.sum ./
RUN go mod download

# 复制源码并编译为静态二进制
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -o sms-server ./main.go


########################
# 2️⃣  Runtime stage
########################
FROM alpine:3.19

WORKDIR /app
RUN apk --no-cache add ca-certificates   # 仅保留根证书，支持 HTTPS

# 拷贝编译好的可执行文件
COPY --from=builder /app/sms-server .

# 默认环境变量（可在 docker run -e … 覆盖）
ENV SERVER_PORT=8080 \
    REDIS_HOST=redis \
    REDIS_PORT=6379 \
    REDIS_DB=0
# ❗敏感信息如 REDIS_PASSWORD 建议运行时注入，不在镜像里硬编码

EXPOSE 8080

# 以非 root 用户运行
RUN adduser -D -g '' appuser && chown -R appuser /app
USER appuser

ENTRYPOINT ["./sms-server"]