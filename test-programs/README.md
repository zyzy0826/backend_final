# 各判題狀態的測試程式

事先備好的 I/O 模式測試提交，每個資料夾對應一種判題狀態，隨時可打包成 zip 提交，用來驗證系統的狀態判定是否正確。

以指南 3.1 的 **「A + B」I/O 題目**為基準（讀入兩整數、輸出其和；測資例如 `3 2`→`5`、`10 20`→`30`）。
`AC` / `WA` 依這組測資設計；`CE` / `RE` / `TLE` / `SE` 與題目內容無關，任何 I/O 題皆適用。

## 對照表

| 資料夾 | 預期狀態 | 觸發方式 | 判定依據（`internal/judge/judge.go`）|
|--------|:--------:|---------|------|
| `ac/` | `AC` | 正確輸出 `a+b` | 輸出與 expected 完全一致 |
| `wa/` | `WA` | 故意輸出 `a-b` | `matchOutput` 不符 |
| `ce/` | `CE` | `main.cpp` 少一個分號 | `cmake --build` 編譯失敗 |
| `re/` | `RE` | 程式 `return 1`（非零 Exit Code）| `r.ExitCode != 0` |
| `tle/` | `TLE` | 無窮迴圈 | `docker run` 逾時被 `docker kill` |
| `se-badcmake/` | `SE` | `CMakeLists.txt` 內 `message(FATAL_ERROR ...)` | configure（`cmake -G`）失敗 |
| `se-nocmake/` | `SE` | 資料夾**故意沒有** `CMakeLists.txt` | `findCMakeRoot` 找不到，編譯前即判 SE |

> `se-nocmake/` 模擬「學生提交時漏掉/刪掉建置檔」的情境。

## 打包

```powershell
# 產生 dist\ac.zip、dist\wa.zip … 等 7 個檔
powershell -File test-programs\pack.ps1
```

或單獨打包某一個（zip 根目錄需直接是 `CMakeLists.txt` / `main.cpp`）：

```powershell
Compress-Archive -Path "test-programs\ac\*" -DestinationPath "test-programs\dist\ac.zip" -Force
```

## 提交並驗證（假設 A+B 題目為 `$ioProblemId`、已有 `$aliceToken`）

```powershell
$expect = [ordered]@{ ac='AC'; wa='WA'; ce='CE'; re='RE'; tle='TLE'; 'se-badcmake'='SE'; 'se-nocmake'='SE' }
$ops = [ordered]@{}
foreach ($k in $expect.Keys) {
  $zip  = "test-programs\dist\$k.zip"
  $resp = curl.exe -s -X POST http://localhost:8080/api/submissions -H "Authorization: Bearer $aliceToken" -F "problem_id=$ioProblemId" -F "file=@$zip" | ConvertFrom-Json
  $ops[$k] = $resp.operator_id
}

Start-Sleep -Seconds 45   # TLE 要跑滿 time_limit，加上排隊，需耐心等
foreach ($k in $ops.Keys) {
  $r   = Invoke-WebRequest -Uri "http://localhost:8080/api/submissions/$($ops[$k])" -Headers @{ Authorization = "Bearer $aliceToken" }
  $got = ($r.Content | ConvertFrom-Json).status
  "{0,-12} 預期 {1,-3} 實得 {2}" -f $k, $expect[$k], $got
}
```

跑完應該七行「預期」與「實得」一致。若某筆仍是 `running`，隔幾秒重跑最後那個 `foreach` 即可。

## 備註

- **RE** 用 `return 1` 是最穩定的非零離開；想要「真的崩潰」版，把 `re/main.cpp` 最後改成 `int* p = nullptr; return *p;`（段錯誤），一樣得 `RE`。
- **TLE** 的逾時是對整個 `docker run` 計時（**含容器啟動**）。容器啟動較慢的機器請把題目 `time_limit` 放寬（20～40s），否則正常程式也可能誤判；但無窮迴圈永不結束，無論門檻多少都必得 `TLE`。
- `dist/` 為打包輸出，不需納入版控。
