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
- [加分項](#加分項)
- [已知限制](#已知限制)

---

## 技術棧

| 層級 | 技術 |
|------|------|
| 語言 | Go 1.25 |
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
├── scripts/
│   └── gen-keys.sh              # 於容器內以 OpenSSL 生成 EC 金鑰對
│
├── Dockerfile
├── docker-compose.yml
├── Makefile
└── .env.example
```

執行期資料一律存放於 Docker 具名資料卷，主機上不留任何目錄：

| 資料卷 | 掛載點 | 內容 |
|--------|--------|------|
| `regs-storage` | `/app/storage` | `uploads/`（上傳的 ZIP）、`workspace/`（解壓後工作目錄）、`problems/`（題目包 ZIP）、`logs/`（三段實體日誌） |
| `keys` | `/app/keys` | EC P-256 金鑰對（`keygen` 服務於首次啟動時生成） |
| `postgres_data` | `/var/lib/postgresql/data` | 資料庫 |

檢視資料卷內容：`docker compose exec app sh`，或 `make export-storage` 匯出至 `./storage-export`。

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
│ created_at  │       │ package_path     │       └──────────────────────┘
└──────┬──────┘       │ created_at       │
       │              └────────┬─────────┘
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
| `GET` | `/api/submissions/{operatorId}/logs/{phase}` | 分段查詢單一階段 log（configure / compile / output） | User（本人或 Admin） |
| `POST` | `/api/submissions/{operatorId}/rejudge` | 重新評測（重設為 pending 並重新排入佇列） | User（本人或 Admin） |

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

上圖為 **I/O 比對模式**（題目測資存於 DB）。若題目上傳了題目包 ZIP（test-based 模式），
流程改為：以題目包的 CMake 專案 configure（`-D SOURCE_ROOT=` 指向學生原始碼）→ build →
以 `ctest --show-only=json-v1` 列舉測試目標，每個 case 在獨立斷網容器中執行，
exit code 0 → PASS、非 0 → WA、125+ → RE、逾時 → TLE。

三段 log 除寫入 DB 外，同時落地為 `storage/logs/{operatorId}/` 下的
`configure.log`、`compile.log`、`output.log` 實體檔案，可由
`GET /api/submissions/{operatorId}/logs/{phase}` 分段查詢。

### 關於 Docker-out-of-Docker（DooD）

app 容器透過掛載 `/var/run/docker.sock` 操控宿主機的 Docker daemon，判題容器是它的**兄弟容器**而非子容器。

由此衍生一個問題：判題容器要看到 app 容器寫入的檔案。傳統做法是 bind mount，但 daemon 解析的是**宿主機路徑**，因此得額外告知 app 宿主機路徑為何（舊版的 `HOST_STORAGE_PATH`），在 Windows 上還得寫成 `/run/desktop/mnt/host/d/...` 這種形式，極易出錯。

本專案改以**共用具名資料卷**解決：

```
app 容器  ──┐
            ├── regs-storage（具名資料卷）
判題容器  ──┘
```

app 將 `regs-storage` 掛載於 `/app/storage`；判題容器則以**卷名**掛載同一個卷。daemon 依名稱解析，全程不涉及任何宿主機路徑，Windows / macOS / Linux 行為完全一致。

判題容器的掛載方式依 daemon 版本而定（`internal/judge/judge.go` 的 `workspaceMount`）：

| Docker Engine | 掛載參數 | 容器內可見範圍 |
|---------------|---------|--------------|
| 25.0+ | `--mount type=volume,source=regs-storage,target=/workspace,volume-subpath=workspace/<operatorId>` | 僅該次提交自己的工作目錄 |
| < 25.0 | `-v regs-storage:/storage`（工作目錄為 `/storage/workspace/<operatorId>`） | 整個資料卷 |

`volume-subpath` 是較嚴格的做法：學生的程式只看得到自己的目錄，看不到其他人的提交。版本偵測於啟動時執行一次。

> 若不使用 Docker 而直接於主機執行伺服器（`go run`），將 `STORAGE_VOLUME` 留空即可回退為 bind mount 模式，此時才需要設定 `HOST_STORAGE_PATH`。

---

## 快速開始

### 前置需求

**只需要 Docker Desktop（或 Docker Engine + Compose v2）。**

本機**不需要**安裝 Go、OpenSSL 或 PostgreSQL 客戶端——編譯、金鑰生成、建表與判題全部在容器內完成。

### 啟動

```bash
docker compose up -d --build     # 或 make up
```

這一道指令會完成：

1. 於 `golang:1.25-alpine` 內編譯伺服器
2. `keygen` 一次性容器以 OpenSSL 生成 EC P-256 金鑰對至 `keys` 資料卷（已存在則保留）
3. 啟動 Postgres 並等待 healthcheck 通過
4. app 啟動時自動套用內嵌的 `schema.sql`（冪等，每次啟動皆執行）
5. app 啟動時自動確認並拉取判題映像檔 `yhlib/cs3060701`
6. 建立管理員帳號（預設 `admin` / `admin1234`）

完成後 API 位於 <http://localhost:8080>（可用 `.env` 的 `APP_PORT` 改變對外埠號）。

> **首次啟動較慢**：需編譯映像檔並拉取判題映像檔（約 GB 級）。可用 `make logs` 觀察進度。

### 常用指令

| 指令 | 用途 |
|------|------|
| `make up` | 建置並啟動全部服務 |
| `make logs` | 追蹤伺服器日誌 |
| `make rebuild` | 改完程式碼後重新編譯並重啟 app |
| `make shell` | 進入 app 容器（storage 位於 `/app/storage`） |
| `make psql` | 連進資料庫 |
| `make test` / `make vet` | 於暫時性 `golang` 容器內執行測試（不需本機 Go） |
| `make export-storage` | 將 storage 資料卷匯出至 `./storage-export` |
| `make down` | 停止服務，保留資料 |
| `make clean` | 停止並清除所有資料卷 |

> Windows 未安裝 `make` 時，直接執行 Makefile 內對應的 `docker compose ...` 指令即可。

### 設定

所有設定皆有可用預設值，`.env` 為選用。需要調整時複製 `.env.example` 即可：

```bash
cp .env.example .env
```

較常用的是 `APP_PORT`（API 對外埠號）與 `DB_PORT`（Postgres 對外埠號），用於避開本機已被佔用的埠。

---

## 文件

完整專案文件位於 [`docs/`](docs/)：

| 文件 | 說明 |
|------|------|
| [`docs/openapi.yaml`](docs/openapi.yaml) | **OpenAPI 3.0.3** API 規格，涵蓋全部 18 個操作（含 rejudge 與分段 log 查詢）、request/response schema、JWT bearer 認證與錯誤碼 |
| [`docs/ERD.md`](docs/ERD.md) | **ERD**（Mermaid）、資料表關聯說明與提交狀態機 |
| [`docs/測試指南.md`](docs/測試指南.md) | **專案操作說明** — Windows/PowerShell 從零到判題的完整測試步驟與常見問題排解 |
| [`docs/期末專案說明.md`](docs/期末專案說明.md) | 原始題目需求與配分 |
| [`docs/判題模式.md`](docs/判題模式.md) | Test-based 判題模式說明 |

> **檢視 OpenAPI**：把 `docs/openapi.yaml` 內容貼到 <https://editor.swagger.io/>，或用 VS Code 的
> OpenAPI (Swagger) 外掛預覽。
> **檢視 ERD / 狀態機**：`docs/ERD.md` 的 Mermaid 圖在 GitHub 上會自動渲染。

---

## 實作狀態

所有核心業務邏輯皆已實作完成，`go build ./...`、`go vet ./...` 與 `go test ./...` 通過。

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
- [x] 題目 — List / Get / Upsert（JSON 測資或 multipart 題目包）/ Delete / GetTestcases
- [x] 提交 — Create（非同步，回 202）/ List / Get / GetSource / GetByUser ＋ `canAccess` 擁有權檢查
- [x] 提交 — GetLog（分段 log，實體檔案優先）/ Rejudge（重設後重新入佇列）
- [x] 統計 — ProblemStats / UserStats
- [x] 上傳大小限制（`MAX_UPLOAD_MB`，提交與題目包皆適用）

### 判題引擎
- [x] Docker CLI runner（`--rm`、Windows path 轉換、network 隔離、TLE 時 `docker kill` 防孤兒容器）
- [x] 執行階段資源限制 — `--memory` / `--cpus` / `--pids-limit`（`JUDGE_MEMORY_LIMIT` / `JUDGE_CPU_LIMIT`）
- [x] `RunJob` — 三階段（configure→SE ∕ build→CE ∕ 執行→AC·WA·RE·TLE），雙模式（I/O 比對 ∕ test-based ctest）
- [x] 三段 log 同步寫入 DB 與實體檔案 `storage/logs/{operatorId}/{configure,compile,output}.log`
- [x] `extractZip`（含 zip-slip 路徑穿越防護、反斜線分隔符正規化，相容 Windows PowerShell 打包的 ZIP）、`done`、`logFrom`、輸出正規化比對

### 測試
- [x] `internal/judge/extract_test.go` — ZIP 反斜線正規化與 zip-slip 防護的回歸測試（`go test ./...` 通過）

### 非同步任務
- [x] `Queue.run` — buffered channel 作為 semaphore 控制最大併發數

### 基礎設施
- [x] Dockerfile（builder `golang:1.25`、runtime 內含 `docker-cli` 供 DooD、`openssl` 供金鑰生成）
- [x] docker-compose — `keygen` 一次性金鑰生成、app + db（healthcheck）、`tools` profile 提供容器化 Go 工具鏈
- [x] 全容器化：不需本機 Go / OpenSSL / psql，`docker compose up -d --build` 即可完整啟動
- [x] 啟動時自動套用 schema（內嵌 `schema.sql`，冪等）與自動拉取判題映像檔
- [x] Makefile（全部指令走 Docker）/ `.env.example` / `.gitignore` / `.dockerignore`

### 文件（評分項）
- [x] OpenAPI 3.0 規格 — [`docs/openapi.yaml`](docs/openapi.yaml)
- [x] ERD — [`docs/ERD.md`](docs/ERD.md)
- [x] 專案操作說明 — [`docs/測試指南.md`](docs/測試指南.md) 及本 README

---

## 加分項

已實作：

- [x] Rejudge API — `POST /api/submissions/{operatorId}/rejudge`（本人或 Admin），重判時重新載入最新題目定義
- [x] 分段日誌 API — `GET /api/submissions/{operatorId}/logs/{phase}`，並落地為實體 log 檔
- [x] 上傳 ZIP 大小限制（`MAX_UPLOAD_MB`）
- [x] 容器 Memory / CPU / PID 資源限制

- [x] JWT 真正登出 — logout 將 token 的 `jti` 加入撤銷清單（`internal/api/middleware/denylist.go`），到期自動清除

尚未實作（選做）：

- [ ] Queue 狀態 API — 查詢佇列深度與 running 數量

---

## 已知限制

- **判題執行檔定位為啟發式**（I/O 模式）：build 完成後以 `find build -perm -u+x ...` 取第一個可執行檔執行，
  適用單一 binary 的專案；多 target 專案可能需調整 `internal/judge/judge.go` 的 `runScript`。
- **TLE 計時包含容器啟動時間**：時限是對整個 `docker run` 計時。在容器啟動較慢的環境
  （如部分 Windows Docker Desktop 主機，實測啟動一個容器可達 10 秒以上），
  題目 `time_limit` 需相應放寬，否則會誤判 TLE。
- **判題容器仍需宿主機 Docker daemon**：app 透過掛載 `/var/run/docker.sock` 產生兄弟容器（DooD）。
  這是刻意的取捨——相較於 Docker-in-Docker，它不需要 privileged 權限，且判題映像檔沿用宿主機快取。
  代價是 app 容器對宿主機 daemon 具有完整控制權。
