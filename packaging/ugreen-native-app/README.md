# UGREEN Native App Packaging

This directory keeps the UGREEN native app packaging skeleton aligned with the official `project.yaml` + `rootfs_*` layout.

## Layout

- `project.yaml`: app metadata and runtime settings
- `icon.png`: package icon used by `ugcli`
- `rootfs_common/`: files shared by all architectures
- `rootfs_amd64/bin/`: Linux AMD64 backend binary
- `rootfs_arm64/bin/`: Linux ARM64 backend binary

## Build Binaries

From the repository root:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\build-ugreen-native-app.ps1
```

This cross-compiles the Go backend into:

- `rootfs_amd64/bin/nasnotify`
- `rootfs_arm64/bin/nasnotify`

## Pack With ugcli

From this directory:

```powershell
..\..\tools\ugcli\ugcli-v1.1.0.12-windows-amd64.exe pack --build 1
```

Adjust the `--build` number as needed for release packaging.
