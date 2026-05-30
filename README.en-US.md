# UGREEN NAS WeChat Notifier

English | [中文](README.md)

UGREEN NAS native app for WeChat-based notifications, status queries, and fixed remote commands.

This project targets a single local UGREEN NAS. It does not provide multi-device management and does not depend on Enterprise WeChat, PushPlus, or public callback services. The app packages a Go backend, a web frontend, and an embedded WeChat gateway into a UGREEN native app.

## Features

- UGREEN native app frontend and Go backend
- Single local UGREEN NAS integration
- Embedded WeChat login, binding, menu, and command handling
- WeChat replies for system status, storage, Docker, processes, backup, power, UPS, fan, and CPU control
- UGREEN UPK packaging scripts for amd64 and arm64

## WeChat Commands

- `菜单`: show supported commands
- `状态`: show NAS system status
- `通知`: send a test notification
- `存储`: show storage usage
- `Docker`: show Docker status
- `进程`: show process summary
- `备份`: show backup status
- `电源`: show power information
- `UPS`: show UPS status
- `测试`: send a test reply
- `风扇1` / `风扇2` / `风扇3`: switch fan mode
- `CPU0` / `CPU1` / `CPU2`: switch CPU mode

## Project Layout

- `cmd/nasnotify`: backend service and HTTP API
- `frontend/ugreen-app`: UGREEN app frontend source
- `internal/nas`: UGREEN NAS API integration and WeChat reply formatting
- `internal/notify`: WeChat binding, menu, and command reply logic
- `internal/wechatgateway`: embedded WeChat login, session, and message gateway
- `packaging/ugreen-native-app`: UGREEN app manifest and rootfs assets
- `scripts`: frontend sync and native app build scripts

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

Generated UPK files are placed under:

```text
packaging/ugreen-native-app/build_dir/pkgs/upk/
```

## Usage

1. Install the generated UPK on the UGREEN NAS.
2. Open the app and finish initialization.
3. Generate a QR code in the WeChat gateway page and scan it in WeChat.
4. Send any message in WeChat to activate the session.
5. Send the binding code shown in the app to finish binding.
6. After binding, send fixed WeChat commands to query or control the NAS.

## Notes

- The NAS must be able to access external HTTPS networks, otherwise WeChat QR login and message sync may fail.
- The project targets a single local UGREEN NAS only.
- The project is still being tested in real environments. Validate it before long-term use.

## License

This project is released under the MIT License. See [LICENSE](LICENSE).
