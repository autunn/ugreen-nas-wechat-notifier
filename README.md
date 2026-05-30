# UGREEN NAS WeChat Notifier

UGREEN NAS native app for WeChat-based notifications and remote status commands.

This project targets a single local UGREEN NAS. It packages a Go backend, an embedded web UI, and a WeChat gateway into a UGREEN native app.

## Features

- UGREEN native app frontend and Go backend
- Single local UGREEN NAS integration
- Embedded WeChat login, binding, menu, and command handling
- WeChat replies for system status, storage, Docker, processes, backup, power, UPS, fan, and CPU control
- UGREEN UPK packaging scripts for amd64 and arm64

## Project Layout

- `cmd/nasnotify`: backend service and HTTP API
- `frontend/ugreen-app`: UGREEN app frontend source
- `internal/nas`: UGREEN NAS API integration and WeChat reply formatting
- `internal/notify`: WeChat binding, menu, and command push helpers
- `internal/wechatgateway`: embedded WeChat login/session/message gateway
- `packaging/ugreen-native-app`: UGREEN app manifest and rootfs assets
- `scripts`: frontend sync and native app build scripts

## WeChat Commands

- `菜单`: show supported commands
- `状态`: show NAS system status
- `通知`: send a test notification
- `存储`: show storage usage
- `Docker`: show Docker status
- `进程`: show process summary
- `备份`: show backup status
- `电源`: show power controls
- `UPS`: show UPS status
- `测试`: send a test reply
- `风扇1` / `风扇2` / `风扇3`: switch fan mode
- `CPU0` / `CPU1` / `CPU2`: switch CPU mode

## Build

Run Go checks:

```powershell
go test ./...
go vet ./...
```

Build UGREEN app artifacts:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\build-ugreen-native-app.ps1 -Version v2026.05.30
```

## License

This project is released under the MIT License. See [LICENSE](LICENSE).
