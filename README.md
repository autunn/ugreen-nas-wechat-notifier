# 绿联 NAS 微信通知助手

[English](README.en-US.md) | 中文

这是一个面向绿联 NAS 的原生应用，用于通过微信完成 NAS 通知推送、状态查询和部分固定指令控制。

项目当前只面向单台本地绿联 NAS，不做多设备管理，不依赖企业微信、PushPlus 或公网回调服务。应用内置 Go 后端、前端页面和微信网关，并通过绿联原生应用格式进行打包。

## 功能特性

- 绿联 NAS 原生应用前端和 Go 后端
- 单台本地绿联 NAS 数据获取和控制
- 内置微信扫码登录、绑定、菜单和消息指令处理
- 支持系统状态、存储、Docker、进程、备份、电源、UPS、风扇和 CPU 模式等微信回复
- 支持 amd64 和 arm64 的绿联 UPK 打包流程

## 微信指令

- `菜单`: 查看支持的指令
- `状态`: 查看 NAS 系统状态
- `通知`: 发送测试通知
- `存储`: 查看存储使用情况
- `Docker`: 查看 Docker 状态
- `进程`: 查看进程摘要
- `备份`: 查看备份状态
- `电源`: 查看电源相关信息
- `UPS`: 查看 UPS 状态
- `测试`: 发送测试回复
- `风扇1` / `风扇2` / `风扇3`: 切换风扇模式
- `CPU0` / `CPU1` / `CPU2`: 切换 CPU 模式

## 项目结构

- `cmd/nasnotify`: 后端服务和 HTTP API
- `frontend/ugreen-app`: 绿联应用前端源码
- `internal/nas`: 绿联 NAS API 集成和微信回复格式化
- `internal/notify`: 微信绑定、菜单和指令回复逻辑
- `internal/wechatgateway`: 内置微信登录、会话和消息网关
- `packaging/ugreen-native-app`: 绿联应用清单和 rootfs 资源
- `scripts`: 前端同步和绿联原生应用构建脚本

## 构建

运行 Go 检查：

```powershell
go test ./...
go vet ./...
```

构建绿联应用产物：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\build-ugreen-native-app.ps1 -Version v2026.05.30
```

构建完成后，UPK 文件会生成在：

```text
packaging/ugreen-native-app/build_dir/pkgs/upk/
```

## 使用说明

1. 在绿联 NAS 中安装构建得到的 UPK。
2. 打开应用并完成初始化设置。
3. 在微信网关页面生成二维码并扫码登录。
4. 在微信端发送任意消息激活会话。
5. 发送页面提示的绑定码完成绑定。
6. 绑定完成后，即可通过微信发送固定指令查询或控制 NAS。

## 注意事项

- NAS 需要能访问外部 HTTPS 网络，否则微信登录二维码和消息收发可能失败。
- 当前项目只针对单台本地绿联 NAS，不支持多设备选择。
- 本项目仍在实测迭代中，建议先在测试环境验证后再长期使用。

## 开源协议

本项目基于 MIT License 开源，详见 [LICENSE](LICENSE)。
