# REGS — Online Judge Backend

CS3060701 期末專案。接收 C++ CMake 專案的 ZIP 壓縮檔，在隔離的 Docker 容器內自動完成編譯、執行與判題，並透過 REST API 對外提供查詢。

---

## 目錄

- [技術棧](#技術棧)
- [專案架構](#專案架構)
- [資料庫設計](#資料庫設計)
- [API 端點](#api-端點)
- [判題流程](#判題流程)
- [快速開始](#快速開始)
- [已完成功能](#已完成功能)
- [待完成事項 To-Do](#待完成事項-to-do)

---

## 技術棧

| 層級 | 技術 |
|------|------|
| 語言 | Go 1.22 |
| HTTP 框架 | Gin |
| 資料庫 | PostgreSQL 16 + pgx/v5 |
| 容器引擎 | Docker SDK (`docker/docker`) |
| 認證 | JWT ES256（EC P-256 Key Pair） |
| 密碼雜湊 | bcrypt |

---

## 專案架構

```
backend_final/
├── cmd/
│   └── server/
│       └── main.go              # 程式進入點，組裝所有元件並啟動 HTTP Server
│
├── internal/
│   ├── config/
│   │   └── config.go            # 從環境變數（.env）讀取所有設定
│   │
│   ├── model/
│   │   └── model.go             # 所有共用資料型別（User, Problem, Submission, Status...）
│   │
│   ├── db/
│   │   ├── db.go                # 建立 pgxpool 連線池
│   │   └── schema.sql           # 資料庫建表 DDL（初始化用）
│   │
│   ├── repository/              # 資料存取層，封裝所有 SQL 查詢
│   │   ├── user_repo.go
│   │   ├── problem_repo.go
│   │   └── submission_repo.go
│   │
│   ├── judge/                   # 核心判題引擎
│   │   ├── docker.go            # Docker 容器建立、執行、取得 log、清除
│   │   └── judge.go             # 完整判題流程（解壓 → cmake → 執行 → 比對）
│   │
│   ├── queue/
│   │   └── queue.go             # FIFO Job Queue + Semaphore 併發控制
│   │
│   └── api/
│       ├── middleware/
│       │   ├── auth.go          # JWT 解析與驗證（ES256）、OptionalAuth
│       │   └── rbac.go          # 角色權限控管（Admin > User > Guest）
│       │
│       ├── handler/             # HTTP 請求處理器
│       │   ├── user_handler.go
│       │   ├── problem_handler.go
│       │   ├── submission_handler.go
│       │   └── stats_handler.go
│       │
│       └── router.go            # Gin 路由設定，掛載 middleware
│
├── storage/                     # 執行期產生（已加入 .gitignore）
│   ├── uploads/                 # 上傳的 .zip 檔案（以 operatorId 命名）
│   └── workspace/               # 解壓後的工作目錄（掛載至 Docker）
│
├── keys/                        # EC 金鑰（已加入 .gitignore）
│   ├── private.pem
│   └── public.pem
│
├── Dockerfile
├── docker-compose.yml
├── Makefile
└── .env.example
```

---

## 資料庫設計

```
┌─────────────┐       ┌──────────────────┐       ┌──────────────────────┐
│    users    │       │     problems     │       │      testcases       │
├─────────────┤       ├──────────────────┤       ├──────────────────────┤
│ id (PK)     │       │ id (PK)          │◄──────│ id (PK)              │
│ username    │       │ title            │  1:N  │ problem_id (FK)      │
│ password    │       │ description      │       │ input                │
│ role        │       │ time_limit (sec) │       │ expected             │
│ created_at  │       │ created_at       │       └──────────────────────┘
└──────┬──────┘       └────────┬─────────┘
       │ 1:N                   │ 1:N
       ▼                       ▼
┌──────────────────────────────────────────────┐
│                  submissions                  │
├──────────────────────────────────────────────┤
│ id (PK)                                      │
│ operator_id (UUID, UNIQUE) ← 對外查詢用      │
│ user_id (FK → users)                         │
│ problem_id (FK → problems)                   │
│ status  pending|running|AC|WA|CE|SE|RE|TLE   │
│ source_path                                  │
│ created_at                                   │
└──────────────────┬───────────────────────────┘
                   │ 1:1
                   ▼
        ┌──────────────────────┐
        │   submission_logs    │
        ├──────────────────────┤
        │ submission_id (PK,FK)│
        │ configure_log        │
        │ compile_log          │
        │ output_log           │
        └──────────────────────┘
```

### 狀態機

```
         ┌──────────┐
  提交 → │ pending  │
         └────┬─────┘
              │ Job Queue 取出
              ▼
         ┌──────────┐
         │ running  │
         └────┬─────┘
              │
   ┌──────────┼──────────────────┐
   ▼          ▼                  ▼
  SE         CE     執行階段結果判定
(cmake -G  (cmake    ┌─────┬────┬────┬────┐
 失敗)      build    │ AC  │ WA │ RE │ TLE│
           失敗)     └─────┴────┴────┴────┘
```

---

## API 端點

### 權限說明

> **Admin > User > Guest**  
> Guest 路由無需 Authorization Header  
> User / Admin 路由需在 Header 加上 `Authorization: Bearer <token>`

### UserInfo

| 方法 | 路徑 | 描述 | 權限 |
|------|------|------|------|
| `POST` | `/api/users/register` | 註冊新帳號 | Guest |
| `POST` | `/api/users/login` | 登入，回傳 JWT | Guest |
| `POST` | `/api/users/logout` | 登出（JWT 無狀態） | User |
| `GET` | `/api/users/me` | 取得自己的個人資料 | User |
| `GET` | `/api/users/{user_id}/submissions` | 取得指定使用者的提交紀錄 | Guest |

### Problem

| 方法 | 路徑 | 描述 | 權限 |
|------|------|------|------|
| `GET` | `/api/problems` | 題目列表 | Guest |
| `GET` | `/api/problems/{problem_id}` | 單一題目詳情 | Guest |
| `PUT` | `/api/problems` | 建立或更新題目（含測資） | Admin |
| `DELETE` | `/api/problems/{problem_id}` | 刪除題目 | Admin |
| `GET` | `/api/problems/{problem_id}/testcases` | 下載題目測資 | Admin |

### Submission

| 方法 | 路徑 | 描述 | 權限 |
|------|------|------|------|
| `POST` | `/api/submissions` | 提交 ZIP，立即回傳 `operatorId` | User |
| `GET` | `/api/submissions` | 查詢個人提交列表 | User |
| `GET` | `/api/submissions/{operatorId}` | 取得判題結果與三段 log | User（本人或 Admin） |
| `GET` | `/api/submissions/{operatorId}/source` | 下載原始 ZIP | User（本人或 Admin） |

### Statistics

| 方法 | 路徑 | 描述 | 權限 |
|------|------|------|------|
| `GET` | `/api/stats/problems/{problem_id}` | 題目統計（總提交、AC/WA/CE...） | Guest |
| `GET` | `/api/stats/users/{user_id}` | 使用者統計（總提交、AC、解題數） | Guest |

---

## 判題流程

```
POST /api/submissions (multipart: file=.zip, problem_id=N)
  │
  ├─ 1. 驗證 JWT + 權限
  ├─ 2. 儲存 ZIP → storage/uploads/{operatorId}.zip
  ├─ 3. 寫入 DB（status = pending）
  ├─ 4. 立即回傳 202 { "operator_id": "..." }   ← 非同步，不等待
  │
  └─ 背景 Job Queue（Semaphore 控制最大併發數）:
       │
       ├─ [status = running]
       ├─ 解壓 ZIP → storage/workspace/{operatorId}/
       ├─ 確認 CMakeLists.txt 存在
       │
       ├─ Docker 容器 #1（network = bridge，允許網路）
       │   指令: cmake -G Ninja -B build
       │   失敗 → [status = SE]，寫入 configure.log
       │
       ├─ Docker 容器 #2（network = bridge）
       │   指令: cmake --build build --verbose
       │   失敗 → [status = CE]，寫入 compile.log
       │
       └─ 對每組測資，各啟動獨立容器（network = none，完全斷網）:
           ├─ 寫入 input.txt 至 workspace
           ├─ 執行 binary < /workspace/input.txt（受 time_limit 限制）
           ├─ 超時 → [status = TLE]
           ├─ exit code != 0 → [status = RE]
           ├─ 輸出不符 → [status = WA]
           └─ 全部通過 → [status = AC]
           寫入 output.log
```

### 關於 Docker-out-of-Docker（DooD）

app 容器透過掛載 `/var/run/docker.sock` 操控宿主機的 Docker daemon。  
volume mount 路徑必須是**宿主機上的路徑**，因此需設定 `HOST_STORAGE_PATH`：

```
# .env 範例（當 app 跑在 Docker 內時）
HOST_STORAGE_PATH=/home/user/backend_final/storage
```

本機直接執行（`go run`）時，`HOST_STORAGE_PATH` 與 `STORAGE_PATH` 相同，不需特別設定。

---

## 快速開始

### 前置需求

- Go 1.22+
- Docker & Docker Compose
- OpenSSL（用於生成 JWT 金鑰）
- PostgreSQL 客戶端（可選，`make migrate` 用）

### 步驟

```bash
# 1. 安裝 Go 依賴 + 生成 EC 金鑰 + 建立 storage 目錄
make setup

# 2. 複製並編輯環境設定
cp .env.example .env
# 修改 DATABASE_URL 等設定

# 3. 啟動資料庫
docker compose up db -d

# 4. 建立資料表
make migrate

# 5. 啟動伺服器
make run
# → Listening on :8080
```

### 使用 Docker Compose 完整部署

```bash
# 設定宿主機 storage 路徑（必須為絕對路徑）
export HOST_STORAGE_PATH=$(pwd)/storage

make docker-up   # 等同於 docker compose up -d --build
make docker-down # 停止並移除 volumes
```

### 生成 JWT 金鑰

```bash
make gen-keys
# 產生 keys/private.pem 與 keys/public.pem
```

或手動執行：

```bash
openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out keys/private.pem
openssl pkey -in keys/private.pem -pubout -out keys/public.pem
```

---

## 已完成功能

### 核心架構

- [x] Go 模組初始化（`module regs`，Go 1.22）
- [x] 環境變數設定（`internal/config/config.go`，支援 `.env`）
- [x] PostgreSQL 連線池（`pgxpool`）
- [x] 資料庫 Schema（users / problems / testcases / submissions / submission_logs）

### 認證與權限

- [x] JWT 簽發（ES256，EC P-256 私鑰）
- [x] JWT 驗證 Middleware（`Auth`、`OptionalAuth`）
- [x] RBAC 角色權限控管（`RequireRole`，Admin > User > Guest 層級）

### 使用者 API

- [x] `POST /api/users/register` — 註冊（bcrypt 雜湊）
- [x] `POST /api/users/login` — 登入，回傳 JWT
- [x] `POST /api/users/logout` — 登出（JWT 無狀態）
- [x] `GET /api/users/me` — 取得個人資料
- [x] `GET /api/users/{user_id}/submissions` — 取得指定使用者提交紀錄

### 題目 API

- [x] `GET /api/problems` — 題目列表
- [x] `GET /api/problems/{problem_id}` — 題目詳情
- [x] `PUT /api/problems` — 建立 / 更新題目（含測資，事務操作）
- [x] `DELETE /api/problems/{problem_id}` — 刪除題目
- [x] `GET /api/problems/{problem_id}/testcases` — 取得測資列表（Admin）

### 提交 API

- [x] `POST /api/submissions` — 接收 ZIP，非同步判題，立即回傳 `operatorId`
- [x] `GET /api/submissions` — 查詢個人提交列表
- [x] `GET /api/submissions/{operatorId}` — 取得判題結果 + 三段 log
- [x] `GET /api/submissions/{operatorId}/source` — 下載原始 ZIP

### 統計 API

- [x] `GET /api/stats/problems/{problem_id}` — 題目統計
- [x] `GET /api/stats/users/{user_id}` — 使用者統計

### 判題引擎

- [x] ZIP 解壓縮（含 zip-slip 路徑安全防護）
- [x] CMakeLists.txt 存在性檢查
- [x] Phase 1：`cmake -G Ninja -B build`（失敗 → SE）
- [x] Phase 2：`cmake --build build --verbose`（失敗 → CE）
- [x] Phase 3：`--network none` 隔離執行，stdin 重導向 input.txt
- [x] 五種狀態判定：AC / WA / CE / SE / RE / TLE
- [x] 三段日誌儲存：configure.log / compile.log / output.log
- [x] 容器逾時強制中止（TLE 判定）
- [x] 容器自動清除（`defer ContainerRemove`）
- [x] 編譯容器與執行容器網段分離（bridge vs none）

### 非同步任務

- [x] FIFO Job Queue（buffered channel，容量 256）
- [x] Semaphore 併發控制（`MAX_CONCURRENT_JOBS`，預設 3）
- [x] Goroutine-based 背景執行

### 基礎設施

- [x] Dockerfile（multi-stage build，alpine）
- [x] docker-compose.yml（app + db，healthcheck，DooD socket 掛載）
- [x] Makefile（`run` / `build` / `setup` / `gen-keys` / `migrate` / `docker-up`）
- [x] `.env.example`
- [x] `.gitignore`（排除 keys/、storage/、.env）

---

## 待完成事項 To-Do

### 必要補完（影響評分）

- [ ] **`go mod tidy`** — 執行後生成 `go.sum`，確保所有依賴版本鎖定
- [ ] **`make migrate` 測試** — 確認 `schema.sql` 能在 PostgreSQL 正確執行
- [ ] **Docker Image 確認** — 拉取 `yhlib/cs3060701` 並確認 Ninja、CMake、Clang 版本
- [ ] **端對端測試** — 用測試用 C++ CMake 專案完整走一遍判題流程

### 功能完善

- [ ] **`GET /api/problems/{problem_id}/testcases` 改為下載 ZIP** — 目前回傳 JSON，需改為打包成 `.zip` 後以 `FileAttachment` 回傳
- [ ] **`PUT /api/problems` 請求格式確認** — 測資（testcases）目前以 JSON body 傳入，若需要改為 multipart（含附件），需調整 handler
- [ ] **重新執行某個 Job** — 加入 `POST /api/submissions/{operatorId}/rejudge`（Admin），允許重新判題（加分項）
- [ ] **Pending 狀態查詢** — `GET /api/submissions/{operatorId}` 回傳 `pending` 時可加入佇列位置資訊
- [ ] **多個 binary 處理** — 若 cmake build 產生多個 executable，目前取第一個；可改為讀取 CMakeLists.txt 或指定規則

### 安全性強化

- [ ] **JWT 黑名單（Logout）** — 目前 logout 為 no-op；若需真正作廢 token，需在 Redis 或 DB 維護黑名單
- [ ] **上傳檔案大小限制** — 在 Gin 或 Nginx 層限制 ZIP 大小（防止超大檔案攻擊）
- [ ] **容器資源限制** — 在 Docker HostConfig 加入 `Memory`、`NanoCPUs` 限制，防止 OOM

### 文件（評分 10 分）

- [ ] **OpenAPI 3.0 規格檔** — 建立 `docs/openapi.yaml`，覆蓋所有 API 端點、request/response schema、錯誤碼
- [ ] **ERD 圖** — 以 Mermaid 或圖片形式加入文件
- [ ] **專案操作說明** — 包含環境設定、金鑰生成、Docker 部署步驟

### 加分項（選做）

- [ ] **Rejudge API** — `POST /api/submissions/{operatorId}/rejudge`
- [ ] **Queue 狀態 API** — 查詢目前佇列深度與 running 數量
- [ ] **Admin 查詢所有提交** — `GET /api/submissions?all=true`（Admin only）
- [ ] **分頁支援** — 提交列表加入 `?page=` / `?limit=` 參數
