#include <iostream>
// RE: 正常編譯與執行，但以非零 Exit Code 結束（判題器據此判 RE）。
// 想改成「真的崩潰」版可換成：int* p = nullptr; return *p;（段錯誤，一樣是 RE）
int main() {
    int a, b;
    std::cin >> a >> b;
    std::cout << a + b << "\n";
    return 1;
}
