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
- [文件](#文件)
- [實作狀態](#實作狀態)
- [加分項](#加分項選做尚未實作)

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

## 文件

完整專案文件位於 [`docs/`](docs/)：

| 文件 | 說明 |
|------|------|
| [`docs/openapi.yaml`](docs/openapi.yaml) | **OpenAPI 3.0.3** API 規格，涵蓋全部 16 個端點、request/response schema、JWT bearer 認證與錯誤碼 |
| [`docs/ERD.md`](docs/ERD.md) | **ERD**（Mermaid）、資料表關聯說明與提交狀態機 |
| [`docs/測試指南.md`](docs/測試指南.md) | **專案操作說明** — Windows/PowerShell 從零到判題的完整測試步驟與常見問題排解 |
| [`docs/期末專案說明.md`](docs/期末專案說明.md) | 原始題目需求與配分 |
| [`docs/判題模式.md`](docs/判題模式.md) | Test-based 判題模式說明 |

> **檢視 OpenAPI**：把 `docs/openapi.yaml` 內容貼到 <https://editor.swagger.io/>，或用 VS Code 的
> OpenAPI (Swagger) 外掛預覽。
> **檢視 ERD / 狀態機**：`docs/ERD.md` 的 Mermaid 圖在 GitHub 上會自動渲染。

---

## 實作狀態

所有核心業務邏輯皆已實作完成，`go build ./...` 與 `go vet ./...` 通過。

### 認證與權限
- [x] JWT 金鑰讀取、`Claims` 定義
- [x] `signToken` — ES256 簽發，24h 到期
- [x] `Auth` / `OptionalAuth` middleware — 強制 ES256 簽章驗證
- [x] `RequireRole` — RBAC（Admin > User 階層）

### Repository（資料存取層）
- [x] `UserRepository` — Create / FindByID / FindByUsername
- [x] `ProblemRepository` — List / FindByID / Upsert（事務＋測資整批替換）/ Delete / GetTestcases
- [x] `SubmissionRepository` — Create（事務）/ FindByOperatorID（JOIN logs）/ ListByUser / UpdateStatus / UpdateStatusWithLogs（事務＋upsert）/ GetProblemStats / GetUserStats

### API Handler
- [x] 使用者 — Register（bcrypt）/ Login / Logout / Me
- [x] 題目 — List / Get / Upsert / Delete / GetTestcases
- [x] 提交 — Create（非同步，回 202）/ List / Get / GetSource / GetByUser ＋ `canAccess` 擁有權檢查
- [x] 統計 — ProblemStats / UserStats

### 判題引擎
- [x] Docker CLI runner（`--rm`、Windows path 轉換、network 隔離）
- [x] `RunJob` — 三階段（configure→SE ∕ build→CE ∕ 執行→AC·WA·RE·TLE）
- [x] `extractZip`（含 zip-slip 路徑穿越防護）、`done`、`logFrom`、輸出正規化比對

### 非同步任務
- [x] `Queue.run` — buffered channel 作為 semaphore 控制最大併發數

### 基礎設施
- [x] Dockerfile / docker-compose（app + db、healthcheck、DooD、db port 開放）/ Makefile / `.env.example` / `.gitignore`

### 文件（評分項）
- [x] OpenAPI 3.0 規格 — [`docs/openapi.yaml`](docs/openapi.yaml)
- [x] ERD — [`docs/ERD.md`](docs/ERD.md)
- [x] 專案操作說明 — [`docs/測試指南.md`](docs/測試指南.md) 及本 README

---

## 加分項（選做，尚未實作）

- [ ] Rejudge API — `POST /api/submissions/{operatorId}/rejudge`（Admin）
- [ ] Queue 狀態 API — 查詢佇列深度與 running 數量
- [ ] JWT 真正登出（token 黑名單，需 Redis 或 DB）
- [ ] 上傳 ZIP 大小限制
- [ ] 容器 Memory / CPU 資源限制

---

## 已知限制

- **判題執行檔定位為啟發式**：build 完成後以 `find build -perm -u+x ...` 取第一個可執行檔執行，
  適用單一 binary 的專案；test-based ∕ 多 target 專案可能需調整 `internal/judge/judge.go` 的 `runScript`。
- **端對端判題需在具備 Docker 的環境實測**：本機需能拉取 `yhlib/cs3060701` 並啟動判題容器。
