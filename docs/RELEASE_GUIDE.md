# MagicDown Release Guide (Dev vs Production)

Hướng dẫn build, kiểm chứng và phát hành MagicDown trên Windows.

---

## 1. Development Build (Nội bộ / Thử nghiệm)

```powershell
# Chạy toàn bộ test trước khi build
go test ./...
node --test extension/*.test.js

# Build chế độ dev (dùng Dev Extension ID từ config/extension-ids.json)
.\scripts\build-release.ps1 -Mode dev

# Kiểm chứng toàn bộ đầu ra trong dist/
.\scripts\verify-release.ps1 -Mode dev
```

---

## 2. Production Build & Release

> **Điều kiện tiên quyết:** Đã submit `MagicDown-extension-chrome.zip` (Chrome Web Store Unlisted) và `MagicDown-extension-edge.zip` (Microsoft Edge Add-ons) và có 2 Production Extension ID chính thức.

### Bước 1: Đồng bộ phiên bản
Cập nhật cùng một số phiên bản (ví dụ `0.2.0`) tại:
- `pkg/version/version.go` (`Version = "0.2.0"`)
- `extension/manifest.json` (`"version": "0.2.0"`)

### Bước 2: Cấu hình Production Extension IDs
Điền `browsers.chrome.prod`, `browsers.edge.prod` (và `store_url`) vào `config/extension-ids.json`, **hoặc** truyền trực tiếp khi chạy build:

```powershell
.\scripts\build-release.ps1 `
  -Mode prod `
  -Version 0.2.0 `
  -ChromeProdExtId "<32-char-chrome-prod-id>" `
  -EdgeProdExtId "<32-char-edge-prod-id>"
```

### Bước 3: Kiểm chứng tự động trước khi phát hành (`-StrictProd`)

```powershell
.\scripts\verify-release.ps1 -Mode prod -StrictProd
```

Đầu ra hợp lệ trong `dist/` bao gồm:
- `magicdown.exe` (Windows GUI subsystem `-H=windowsgui`)
- `magicdown-host.exe` (Native Messaging stdio host)
- `MagicDown-windows-amd64.zip` (gồm binaries + `install.ps1` + `uninstall.ps1` + `config/extension-ids.json`)
- `MagicDown-extension-chrome.zip` (MV3 sạch, đã strip `"key"`, không có `*.test.js`)
- `MagicDown-extension-edge.zip` (MV3 sạch, đã strip `"key"`, không có `*.test.js`)
- `manifests\com.magicdown.downloader.chrome.json` & `manifests\com.magicdown.downloader.edge.json`
- `checksums.txt` (SHA-256 cho toàn bộ artifact)
- `install.ps1` & `uninstall.ps1`

### Bước 4: Phát hành GitHub Release

```powershell
.\scripts\publish-release.ps1 -Version 0.2.0
```
