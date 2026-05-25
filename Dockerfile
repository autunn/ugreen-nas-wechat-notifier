# 核心秘诀：强制使用宿主机(x86)原生环境作为 builder，绕过极慢的 QEMU 模拟器
FROM --platform=$BUILDPLATFORM golang:alpine AS builder

# 接收 GitHub Actions 传进来的版本号
ARG APP_VERSION=v2026.05.01
# 核心秘诀：接收 Docker 自动传进来的目标架构 (例如 amd64, arm64, arm)
ARG TARGETARCH

WORKDIR /app
RUN apk add --no-cache git
COPY . .

RUN go mod download
# 核心秘诀：直接指定 GOARCH=$TARGETARCH 进行极速交叉编译
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build \
    -ldflags "-s -w -X main.Version=${APP_VERSION}" \
    -o nasnotify-go-app .

# ==========================================
# 最终运行阶段：拉取对应架构的 alpine 基础镜像
FROM alpine:latest
WORKDIR /app

# 配置时区为上海
RUN apk add --no-cache ca-certificates tzdata \
    && cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime \
    && echo "Asia/Shanghai" > /etc/timezone

# 把极速编译好的二进制文件复制过来
COPY --from=builder /app/nasnotify-go-app .

# 复制前端模板文件夹
COPY templates ./templates

EXPOSE 5080
VOLUME ["/app/data", "/app/config"]
CMD ["./nasnotify-go-app"]