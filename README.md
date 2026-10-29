# LinkSaverBack

[![Go](https://img.shields.io/badge/Go-1.22%2B-00ADD8?style=flat-square&logo=go)](https://golang.org/)
[![Framework](https://img.shields.io/badge/Framework-Gin-008ECF?style=flat-square&logo=gin)](https://gin-gonic.com/)
[![Database](https://img.shields.io/badge/Database-SQLite%20(WAL)-003B57?style=flat-square&logo=sqlite)](https://sqlite.org/)
[![Platform](https://img.shields.io/badge/Platform-Linux%20%7C%20Windows%20%7C%20macOS-blue?style=flat-square)](#)
[![Clients](https://img.shields.io/badge/Clients-Nintendo%20Switch%20%7C%20PC%20%7C%20Android-informational?style=flat-square)](#)

Central synchronization microservice and save file coordinator for the cross-platform LinkSaver ecosystem. LinkSaverBack coordinates game saves across Nintendo Switch [(`nx-LinkSaver`)](https://github.com/Quimaerical/nx-LinkSaver) and PC/Android [(`LinkSaverApp`)](https://github.com/Quimaerical/LinkSaverApp) via unified logical tags, automated collision resolution, and an embedded management dashboard.

---

## Quickstart

### Prerequisites

- Go 1.22 or higher
- GCC / C++ build essentials (required for SQLite CGO compilation)

### 1. Build Single Binary

```bash
cd LinkSaverBack
go build -o bin/linksaver cmd/server/main.go
```

The resulting binary (`bin/linksaver`) is fully self-contained, with all HTML templates and static dashboard assets embedded directly via Go `embed.FS`.

### 2. Run the Service

```bash
./bin/linksaver
```

By default, the server listens on `http://localhost:8080`. 
Open `http://localhost:8080` in your browser to access the management dashboard.

---

## Configuration

LinkSaverBack is configured through environment variables:

| Variable | Default | Description |
| :--- | :--- | :--- |
| `PORT` | `8080` | Port on which the HTTP server listens. |
| `SAVESYNC_DATA_DIR` | `./storage/saves` | Base directory on the host where save directories are stored. |
| `DATABASE_PATH` | `./storage/linksaver.db` | File path for the SQLite database. |
| `LINK_SAVER_AUTH_TOKEN` | *(empty)* | Optional Bearer authorization token. If unset, the service accepts unauthenticated LAN requests. |

### Example with Custom Storage and Auth Token

```bash
export PORT=9000
export SAVESYNC_DATA_DIR="/mnt/saves"
export DATABASE_PATH="/mnt/saves/linksaver.db"
export LINK_SAVER_AUTH_TOKEN="your-secure-secret-token"

./bin/linksaver
```

---

## Connecting Clients

The LinkSaver ecosystem uses **Logical Directory Tags** (`directory_tag`) so clients never expose physical local paths to one another. Each platform maps its local folder to a common tag on the server (e.g., `retroarch-saves`).

```
[Nintendo Switch]     sdmc:/retroarch/cores/savefiles  -->  [Tag: retroarch-saves]
[Windows / Linux]     C:/Emulation/RetroArch/saves     -->  [Tag: retroarch-saves]  --> /storage/saves/retroarch-saves/
[Android Client]      /sdcard/RetroArch/saves          -->  [Tag: retroarch-saves]
```

### Option A: Local Network Connection (LAN)

If running in your home network, find the host machine's IP address:
```bash
ip addr show   # Linux / macOS
ipconfig       # Windows
```

Use `http://<SERVER_IP>:8080` as the Server URL in your clients.

### Option B: Remote Connection via Cloudflare Tunnel

For secure external access without opening router ports:

1. Install `cloudflared` on the host machine.
2. Authenticate and create a tunnel:
   ```bash
   cloudflared tunnel create linksaver
   ```
3. Route traffic to the local LinkSaverBack daemon:
   ```yaml
   # ~/.cloudflared/config.yml
   tunnel: <TUNNEL_UUID>
   credentials-file: /home/user/.cloudflared/<TUNNEL_UUID>.json
   ingress:
     - hostname: linksaver.yourdomain.com
       service: http://localhost:8080
     - service: http_status:404
   ```
4. Start the tunnel:
   ```bash
   cloudflared tunnel run linksaver
   ```
5. Use `https://linksaver.yourdomain.com` as the Server URL in all clients.

---

### Connecting Nintendo Switch (`sw-LinkSaver`)

On the Nintendo Switch microSD card, client configuration is read from `sdmc:/switch/LinkSaver/config.json`:

```json
{
  "server_url": "https://linksaver.yourdomain.com",
  "auth_token": "your-secure-secret-token",
  "directories": [
    {
      "tag": "retroarch-saves",
      "display_name": "RetroArch Cores",
      "local_path": "/retroarch/cores/savefiles",
      "extensions": [".srm", ".sav", ".state"]
    },
    {
      "tag": "azahar-saves",
      "display_name": "Azahar 3DS",
      "local_path": "/switch/azahar/saves",
      "extensions": [".sav", ".dsv"]
    }
  ]
}
```

1. Launch `sw-LinkSaver` from the Homebrew Menu.
2. If first launched, enter your Server URL and Token using the on-screen keyboard.
3. Tap **Test Ping** to confirm connectivity (`GET /api/v1/ping`).
4. Tap **Sync Now** to start bidirectional synchronization.

---

### Connecting Desktop & Mobile Clients (`LinkSaverApp`)

1. Launch **LinkSaverApp** on Windows, Linux, or Android.
2. Navigate to **Settings** and set:
   - **Server Endpoint**: `https://linksaver.yourdomain.com` (or `http://<LAN_IP>:8080`)
   - **Access Token**: *(matching `LINK_SAVER_AUTH_TOKEN` if enabled)*
3. Add a Directory Mapping:
   - Choose a logical tag (e.g., `retroarch-saves`).
   - Pick your local emulator folder (on Android, select via Storage Access Framework).
4. Tap **Verify Connection** and then **Synchronize**.

---

## Core Protocol Endpoints

All client requests interact with `/api/v1`:

| Method | Endpoint | Description |
| :--- | :--- | :--- |
| `GET` | `/api/v1/ping` | Healthcheck and latency verification. Returns `{"status":"ok","version":"1.0.0"}`. |
| `POST` | `/api/v1/directories/register` | Registers or ensures existence of a directory tag. |
| `GET` | `/api/v1/directories` | Lists all registered logical tags and storage paths. |
| `PUT` | `/api/v1/directories/:tag/storage-path` | Relocates physical disk storage dynamically and migrates files. |
| `GET` | `/api/v1/sync/manifest/:tag` | Retrieves active file list with sizes, timestamps, and SHA-256 hashes. |
| `POST` | `/api/v1/sync/upload/:tag` | Multipart upload with atomic write and collision resolution. |
| `GET` | `/api/v1/sync/download/:tag?path=...` | Streams binary save file to client. |
| `GET` | `/api/v1/vault/:tag/history` | Lists all save versions, active files, and conflict backups. |
| `POST` | `/api/v1/vault/:tag/restore` | Promotes a previous conflict backup back to active status. |
| `GET` | `/api/v1/logs` | Audit trail of sync events (Switch, Desktop, Android). |

### Save Collision Resolution Rule

When an incoming save file has the same name but different SHA-256 content:
- **Larger size in bytes** is preserved as active under the original file name (`game.sav`).
- **Smaller or redundant versions** are preserved as inactive backups with an incremental suffix: `game (1).sav`, `game (2).sav`.
- If sizes are equal, the version with the newer remote modification timestamp is prioritized.

---

## Automated Tests

Run the full automated test suite (including unit tests for collision rules, directory migration, and HTTP integration tests):

```bash
cd LinkSaverBack
go test -v ./...
```

Run with Go's race detector to ensure concurrency safety:

```bash
go test -race ./...
```
