# NasNotify-Go 聚合通知中心

基于 Go 语言重构的高性能 NAS 聚合通知与控制中心。支持绿联 (UGreen)、极空间 (ZSpace) 和飞牛 (FnOS) 等多平台设备的系统监控、消息推送以及企业微信深度交互控制。

## ✨ 核心特性

- **多平台支持**: 统一管理绿联、极空间、飞牛 NAS 的系统通知。
- **设备状态监控**: 实时检测设备在线/离线状态，断网告警与恢复通知。
- **企业微信深度集成**:
  - 自动创建企业微信自定义菜单。
  - 支持接收外部通用 Webhook 触发。
  - 图文卡片推送。
- **绿联深度控制 (UGreen Deep API)**:
  - **监控**: 系统概览、存储状态、UPS 电源。
  - **服务**: Docker 容器状态、系统进程 (TOP 5)、备份任务状态。
  - **控制**: 电源与休眠配置、风扇模式控制 (静音/正常/全速)、CPU 频率模式切换 (高性能/均衡/节能)。
- **极致轻量**: 采用标准 Go 工程化结构重构，极低资源占用，支持跨架构多平台运行 (amd64, arm64, arm/v7)。

## 📂 目录结构

本项目采用标准的 Go 语言项目布局：

```text
nasnotify_go/
├── cmd/
│   └── nasnotify/
│       └── main.go          # 程序的总入口
├── internal/                # 内部私有业务逻辑
│   ├── api/                 # Webhook 接收与路由处理 (解决底层循环依赖)
│   ├── config/              # 配置解析与全局读写锁管理
│   ├── crypto/              # 加解密算法库 (AES-CBC/AES-GCM/RSA/MD5/SHA1等)
│   ├── nas/                 # NAS 平台的独立核心逻辑 (UGreen/ZSpace/FnOS)
│   ├── notify/              # 企业微信接口交互、Token 管理与卡片推送
│   └── utils/               # 网络端口检查、设备在线状态等通用工具
├── templates/               # 前端 Web 配置页面模板
├── data/                    # 运行时产生的日志与 Token 数据 (.gitignore 已忽略)
├── config/                  # 配置文件目录 (包含 config.json)
├── Dockerfile               # 多架构 Docker 交叉编译构建脚本
└── go.mod / go.sum

## 🚀 Docker 部署

推荐使用 Docker Compose 进行快速部署，镜像支持 `amd64`、`arm64` 和 `arm/v7`。

```yaml
version: '3.8'
services:
  nasnotify:
    image: ghcr.io/你的GitHub用户名/nasnotify-go:latest  # 请替换为实际的镜像路径
    container_name: nasnotify-go
    restart: unless-stopped
    network_mode: bridge
    ports:
      - "5080:5080"
    volumes:
      - ./config:/app/config
      - ./data:/app/data
    environment:
      - TZ=Asia/Shanghai

启动后，在浏览器中访问 http://<你的IP>:5080 进入 Web 配置界面。

默认管理员密码：admin (请在首次登录后在配置界面中修改并保存)。

## 💬 企业微信交互指令

在企业微信应用中，你可以直接点击底部自定义菜单获取各项监控信息，也可以在聊天输入框内发送以下文本指令进行快速系统控制：

- **风扇控制**
  - `风扇 1`: 切换为静音模式
  - `风扇 2`: 切换为正常模式
  - `风扇 3`: 切换为全速模式
- **CPU 模式控制**
  - `CPU 0`: 切换为高性能模式
  - `CPU 1`: 切换为均衡模式
  - `CPU 2`: 切换为节能模式

## 🛠️ 构建与开发

本项目通过 GitHub Actions 自动进行跨平台编译并发布到 GitHub Container Registry (GHCR)。
如果你需要在本地环境(Windows/Linux)进行编译开发：

```bash
# 1. 下载依赖库
go mod download

# 2. 编译可执行文件 (指定入口为 cmd/nasnotify)
go build -ldflags "-s -w -X main.Version=v2026.05.x" -o nasnotify-go-app ./cmd/nasnotify

# 3. 运行
./nasnotify-go-app
```
