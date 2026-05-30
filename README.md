# NasNotify-Go

UGREEN native app for a single local NAS.

## Current Scope

- UGREEN native app frontend and Go backend
- Single local UGREEN NAS only
- Embedded WeChat gateway, no PushPlus or enterprise WeChat app
- Fixed WeChat commands for status, storage, Docker, processes, backup, power, UPS, fan, and CPU control

## Project Layout

- `cmd/nasnotify`: backend service and HTTP API
- `frontend/ugreen-app`: packaged UGREEN app frontend
- `internal/nas`: UGREEN NAS API integration and WeChat reply formatting
- `internal/notify`: WeChat binding, menu, and command push helpers
- `internal/wechatgateway`: embedded WeChat login/session/message gateway
- `packaging/ugreen-native-app`: UGREEN app manifest, rootfs, and generated UPK output
- `scripts`: UGREEN frontend sync and native app build scripts

## Commands

- `菜单`
- `状态`
- `通知`
- `存储`
- `Docker`
- `进程`
- `备份`
- `电源`
- `UPS`
- `测试`
- `风扇1` / `风扇2` / `风扇3`
- `CPU0` / `CPU1` / `CPU2`
