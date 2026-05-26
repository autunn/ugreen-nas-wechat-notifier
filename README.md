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
```

## 🚀 Docker 部署

推荐使用 Docker Compose 进行快速部署，镜像支持 `amd64`、`arm64` 和 `arm/v7`。

```yaml
version: '3.8'
services:
  nasnotify:
    image: ghcr.io/autunn/nasnotify-go:latest  # 请替换为实际的镜像路径
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
```

## 🌐 网页配置说明与关键点

成功部署并登录 Web 管理后台（默认密码：`admin`）后，您需要在可视化界面中配置以下核心参数。请特别注意以下关键点：

### 1. 安全与全局设置
- **管理员密码**: ⚠️ **关键**：首次登录后，请务必立即修改默认的 `admin` 密码，防止系统被未授权访问。
- **抓取间隔**: 控制后台定时轮询各 NAS 最新消息的时间频率（默认建议 5 分钟）。间隔太短可能导致 NAS 接口频繁响应，太长则通知会有延迟。

### 2. 企业微信参数与回调配置 (核心难点)
这里分为两组参数，分别负责“主动推送消息”和“接收微信指令”：

- **主动推送参数 (负责发图文卡片)**:
  - **CorpID (企业ID) / AgentID (应用ID) / CorpSecret (应用密钥)**: 本程序后台通过这三个参数换取 Token，从而有权限向您的微信客户端推送 NAS 的各项告警与通知卡片。

- **⚠️ 接收指令与回调配置 (负责菜单点击与文本控制)**:
  - **Token / EncodingAESKey**: 用于双向安全验证与消息加解密。
  - **企业微信后台的「URL (接收地址)」**: **这是最关键的一步**。您需要登录企业微信管理后台，进入「应用管理」->「选择您的自建应用」->「接收消息」-> 点击「设置API接收」，在 **URL** 输入框中填入本程序完整的公网回调地址：
    ```text
    http://<您的外网IP或域名>:5080/wx-receive
    ```
  - **配置步骤**:
    1. 在本程序的网页后台随机生成或自行填写 `Token` 和 `EncodingAESKey` 并保存。
    2. 将本程序网页后台的 `Token` 和 `EncodingAESKey` 完完整整地复制到企业微信后台对应的输入框中。
    3. 在企业微信后台填好上述 `http://...:5080/wx-receive` 地址后，点击「保存」。企业微信服务器会自动向该地址发送一条验证请求，本程序收到后会自动解密并回传，通过后即可激活双向通信。

### 3. 存储与 NAS 设备添加
系统支持同时添加多台、多品牌 NAS 设备。不同平台的必填参数有所不同：
- **绿联 (UGreen)**: 需配置内网 IP 及端口（如 `192.168.1.10:9999`）、拥有足够权限的本地账户及密码。
- **极空间 (ZSpace)**: ⚠️ **关键**：除了 IP 端口，极空间由于接口限制，**必须填入有效的 Cookie**。建议在浏览器登录极空间网页版后，通过 F12 抓包获取持久化 Cookie 填入。
- **飞牛 (FnOS)**: 需填入服务器 IP 地址、账户及密码。系统会自动处理飞牛的 WebSocket 登录加密与 Token 续期。

> 💡 **生效机制**：配置完成后，点击页面底部的「保存配置」。系统不仅会保存数据，还会**自动在后台向你的企业微信发起请求，生成最新的底部自定义控制菜单**（无需手动去企业微信后台创建菜单）。

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
目前仅适配绿联系统状态获取、其他品牌欢迎大佬推送

## 🤝 致谢 (Acknowledgments)

本项目的诞生离不开开源社区的贡献，特别感谢以下开源项目提供的灵感与 API 参考：

- [bilibili-koryking/nasnotify](https://github.com/bilibili-koryking/nasnotify) - 提供了各大 NAS 平台**通知获取**的优秀实现思路与代码参考。
- [xbclub/ugreen-monitor](https://github.com/xbclub/ugreen-monitor) - 提供了绿联深层 API (UGreen Deep API) 的相关接口分析与参考。
