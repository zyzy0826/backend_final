#include <iostream>
// SE（缺 CMakeLists）: 這個資料夾「故意沒有」CMakeLists.txt，模擬學生提交時漏掉/刪掉建置檔。
// 判題器的 findCMakeRoot 找不到根目錄的 CMakeLists.txt，會在編譯前就判 SE。
int main() {
    int a, b;
    std::cin >> a >> b;
    std::cout << a + b << "\n";
    return 0;
}
