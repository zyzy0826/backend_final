# 把 test-programs\ 底下每個狀態資料夾各打包成 dist\<狀態>.zip
# 用法（在專案根目錄或 test-programs\ 皆可）：  powershell -File test-programs\pack.ps1
$root = $PSScriptRoot
$dist = Join-Path $root "dist"
Remove-Item $dist -Recurse -Force -ErrorAction SilentlyContinue
New-Item -ItemType Directory $dist -Force | Out-Null

Get-ChildItem $root -Directory | Where-Object { $_.Name -ne 'dist' } | ForEach-Object {
    $zip = Join-Path $dist "$($_.Name).zip"
    Compress-Archive -Path (Join-Path $_.FullName '*') -DestinationPath $zip -Force
}

Write-Host "已產生："
Get-ChildItem $dist -Filter *.zip | Select-Object Name
