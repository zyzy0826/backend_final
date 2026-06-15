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
| 容器引擎 | Docker CLI via `os/exec`（`docker run --rm`） |
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

## 已完成功能（骨架）

### 核心架構

- [x] Go 模組初始化（`module regs`）
- [x] 環境變數設定（`internal/config/config.go`，支援 `.env`）
- [x] PostgreSQL 連線池（`pgxpool`，`internal/db/db.go`）
- [x] 資料庫 Schema（`internal/db/schema.sql`）
- [x] 所有資料型別定義（`internal/model/model.go`）

### 認證與權限

- [x] JWT 金鑰讀取（`LoadPrivateKey` / `LoadPublicKey`，`middleware/auth.go`）
- [x] `Claims` struct 定義（UserID / Username / Role）
- [ ] JWT 簽發 `signToken`（`handler/user_handler.go`）
- [ ] JWT 驗證 Middleware（`Auth`、`OptionalAuth`）
- [ ] RBAC 角色判斷（`RequireRole`，`middleware/rbac.go`）

### Repository（資料存取層）

- [x] Struct 定義與 constructor（`UserRepository`、`ProblemRepository`、`SubmissionRepository`）
- [ ] `UserRepository.Create` / `FindByID` / `FindByUsername`
- [ ] `ProblemRepository.List` / `FindByID` / `Upsert` / `Delete` / `GetTestcases`
- [ ] `SubmissionRepository.Create` / `FindByOperatorID` / `ListByUser`
- [ ] `SubmissionRepository.UpdateStatus` / `UpdateStatusWithLogs`
- [ ] `SubmissionRepository.GetProblemStats` / `GetUserStats`

### 使用者 API

- [x] 路由掛載（`router.go`）
- [ ] `POST /api/users/register` — 註冊（bcrypt 雜湊）
- [ ] `POST /api/users/login` — 登入，回傳 JWT
- [ ] `POST /api/users/logout` — 登出
- [ ] `GET /api/users/me` — 取得個人資料
- [ ] `GET /api/users/{user_id}/submissions` — 指定使用者的提交紀錄

### 題目 API

- [x] 路由掛載（`router.go`）
- [ ] `GET /api/problems` — 題目列表
- [ ] `GET /api/problems/{problem_id}` — 題目詳情
- [ ] `PUT /api/problems` — 建立 / 更新題目（含測資，需用事務）
- [ ] `DELETE /api/problems/{problem_id}` — 刪除題目
- [ ] `GET /api/problems/{problem_id}/testcases` — 取得測資（Admin）

### 提交 API

- [x] 路由掛載（`router.go`）
- [ ] `POST /api/submissions` — 接收 ZIP，非同步判題，立即回傳 `operatorId`
- [ ] `GET /api/submissions` — 查詢個人提交列表
- [ ] `GET /api/submissions/{operatorId}` — 判題結果 + 三段 log（需驗證擁有權）
- [ ] `GET /api/submissions/{operatorId}/source` — 下載原始 ZIP

### 統計 API

- [x] 路由掛載（`router.go`）
- [ ] `GET /api/stats/problems/{problem_id}` — 題目統計
- [ ] `GET /api/stats/users/{user_id}` — 使用者統計

### 判題引擎

- [x] Docker CLI runner（`internal/judge/docker.go`，`--rm` 容器，Windows path 轉換）
- [x] `Judge` struct、`JobInput` struct、`New` constructor
- [ ] `RunJob` — 完整三階段判題流程（cmake configure → build → 執行）
- [ ] `extractZip` — ZIP 解壓縮（需含 zip-slip 路徑安全防護）
- [ ] `done` — 更新狀態與三段日誌
- [ ] `logFrom` — 合併 stdout + stderr

### 非同步任務

- [x] `Queue` struct、`Job` struct、`New` constructor、`Push` 方法
- [ ] `Queue.run` — Semaphore 併發控制（buffered channel 模式）

### 基礎設施

- [x] Dockerfile（multi-stage build，alpine）
- [x] docker-compose.yml（app + db，healthcheck，DooD socket 掛載）
- [x] Makefile（`run` / `build` / `setup` / `gen-keys` / `migrate` / `docker-up`）
- [x] `.env.example`
- [x] `.gitignore`（排除 keys/、storage/、.env）

---

## 待完成事項 To-Do

### 核心業務邏輯實作

#### `internal/repository/user_repo.go`
- [ ] `Create` — INSERT INTO users，回傳建立的 User
- [ ] `FindByID` — SELECT by id
- [ ] `FindByUsername` — SELECT by username

#### `internal/repository/problem_repo.go`
- [ ] `List` — SELECT all problems ORDER BY id
- [ ] `FindByID` — SELECT by id，找不到回傳錯誤
- [ ] `Upsert` — 事務：UPDATE（id > 0）或 INSERT，再刪除舊測資並批次 INSERT
- [ ] `Delete` — DELETE by id
- [ ] `GetTestcases` — SELECT testcases by problem_id

#### `internal/repository/submission_repo.go`
- [ ] `Create` — 事務：INSERT submission + INSERT submission_logs（空行）
- [ ] `FindByOperatorID` — JOIN submissions LEFT JOIN submission_logs WHERE operator_id = $1
- [ ] `ListByUser` — SELECT by user_id ORDER BY created_at DESC
- [ ] `UpdateStatus` — UPDATE status WHERE id = $1
- [ ] `UpdateStatusWithLogs` — 事務：UPDATE submissions + UPDATE submission_logs
- [ ] `GetProblemStats` — COUNT(*) FILTER (WHERE status = ...) GROUP
- [ ] `GetUserStats` — COUNT(*) + COUNT(DISTINCT problem_id) FILTER (WHERE status = 'AC')

#### `internal/api/middleware/auth.go`
- [ ] `parseToken` — 從 `Authorization: Bearer <token>` 提取並驗證 ES256 JWT
- [ ] `setClaims` — 將 Claims（user_id、username、role）寫入 gin.Context
- [ ] `Auth` — 驗證失敗則 `c.AbortWithStatus(401)`
- [ ] `OptionalAuth` — 驗證成功才寫 Claims，否則繼續

#### `internal/api/middleware/rbac.go`
- [ ] `RequireRole` — 從 context 讀取 role，比對 `roleLevel` map，不足則 403

#### `internal/api/handler/user_handler.go`
- [ ] `Register` — Bind JSON → bcrypt hash → userRepo.Create → 201
- [ ] `Login` — Bind JSON → FindByUsername → CompareHashAndPassword → signToken → 200
- [ ] `Logout` — JWT 無狀態，回傳 200 + message
- [ ] `Me` — 從 context 取 user_id → userRepo.FindByID → 200
- [ ] `signToken` — 建立 Claims（24h 到期），jwt.NewWithClaims(ES256).SignedString

#### `internal/api/handler/problem_handler.go`
- [ ] `List` — problemRepo.List → 200 JSON
- [ ] `Get` — 解析 :problem_id → problemRepo.FindByID → 200 或 404
- [ ] `Upsert` — Bind JSON（id, title, description, time_limit, testcases）→ problemRepo.Upsert → 200
- [ ] `Delete` — 解析 :problem_id → problemRepo.Delete → 204
- [ ] `GetTestcases` — 解析 :problem_id → problemRepo.GetTestcases → 200 JSON

#### `internal/api/handler/submission_handler.go`
- [ ] `Create` — 取 user_id、解析 problem_id、接收 .zip → 存檔 → submissionRepo.Create → queue.Push → 202
- [ ] `List` — 取 user_id → submissionRepo.ListByUser → 200
- [ ] `Get` — 解析 :operatorId → FindByOperatorID → canAccess 檢查 → 200
- [ ] `GetSource` — 同上 + c.FileAttachment
- [ ] `GetByUser` — 解析 :user_id → ListByUser → 200
- [ ] `canAccess` — role == admin 或 user_id == ownerID

#### `internal/api/handler/stats_handler.go`
- [ ] `ProblemStats` — 解析 :problem_id → submissionRepo.GetProblemStats → 200
- [ ] `UserStats` — 解析 :user_id → submissionRepo.GetUserStats → 200

#### `internal/judge/judge.go`
- [ ] `RunJob` — 完整三階段判題流程：
  - 設 status = running
  - `os.MkdirAll` workspace，結束後 `defer os.RemoveAll`
  - `extractZip` 解壓 ZIP
  - 確認 CMakeLists.txt 存在，否則 SE
  - Phase 1：`cmake -G Ninja -B build`（network=bridge, timeout=60s），失敗 → SE
  - Phase 2：`cmake --build build --verbose`（network=bridge, timeout=120s），失敗 → CE
  - Phase 3：每個 testcase 寫 input.txt，執行 binary（network=none），比對輸出 → AC/WA/RE/TLE
  - 呼叫 `done` 寫入最終狀態與三段 log
- [ ] `extractZip` — zip.OpenReader → 逐檔 path prefix 檢查（防 zip-slip）→ 解壓
- [ ] `done` — 呼叫 `subRepo.UpdateStatusWithLogs`
- [ ] `logFrom` — 回傳 `r.Stdout + r.Stderr`（r 為 nil 時回傳空字串）

#### `internal/queue/queue.go`
- [ ] `run` — `sem := make(chan struct{}, maxConcurrent)`，for range q.jobs 取出 job，`sem <- struct{}{}`，goroutine 執行 RunJob，defer `<-sem`

### 測試與驗證

- [ ] `go mod tidy` — 確認 go.sum 正確
- [ ] `make migrate` — 確認 schema.sql 在 PostgreSQL 正確執行
- [ ] 拉取 `yhlib/cs3060701` image，確認 CMake、Ninja、Clang 版本
- [ ] 端對端測試 — 用 C++ CMake 專案走完完整判題流程

### 文件（評分 10 分）

- [ ] OpenAPI 3.0 規格檔 — `docs/openapi.yaml`，含所有 endpoint、request/response schema、錯誤碼
- [ ] ERD 圖（Mermaid 或圖片）

### 加分項（選做）

- [ ] Rejudge API — `POST /api/submissions/{operatorId}/rejudge`（Admin）
- [ ] Queue 狀態 API — 查詢佇列深度與 running 數量
- [ ] JWT 真正登出（token 黑名單，需 Redis 或 DB）
- [ ] 上傳 ZIP 大小限制
- [ ] 容器 Memory / CPU 資源限制
