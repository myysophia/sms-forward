# syntax=docker/dockerfile:1

########################
# ── 1️⃣ Build stage ── #
########################
FROM golang:1.22-alpine AS builder

WORKDIR /app

# 如需拉私有包可安装 git；tzdata 让日志按本地时区显示（可选）
RUN apk add --no-cache git tzdata

# 先复制 go.mod/go.sum 并拉依赖，充分利用 Docker 层缓存
COPY go.mod go.sum ./
RUN go mod download

# 再复制源码并编译；CGO_ENABLED=0 生成静态二进制
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -o sms-server ./main.go


##########################
# ── 2️⃣ Runtime stage ── #
##########################
FROM alpine:3.19

WORKDIR /app

# 仅保留根证书，供 HTTPS 请求使用
RUN apk --no-cache add ca-certificates

# 拷贝编译好的二进制
COPY --from=builder /app/sms-server .

# （可选）若想打包默认配置，取消下一行注释
# COPY .env .

# ==== 环境变量默认值（可覆盖） ====
ENV SERVER_PORT=8080 \
    REDIS_HOST=redis \
    REDIS_PORT=6379 \
    REDIS_PASSWORD= \
    REDIS_DB=0

EXPOSE 8080

# 建议使用非 root 身份运行
RUN adduser -D -g '' appuser && chown -R appuser /app
USER appuser

ENTRYPOINT ["./sms-server"]