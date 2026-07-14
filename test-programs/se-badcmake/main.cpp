#include <iostream>
// 這份程式本身是對的；SE 由 CMakeLists.txt 的 message(FATAL_ERROR ...) 在 configure 階段觸發
int main() {
    int a, b;
    std::cin >> a >> b;
    std::cout << a + b << "\n";
    return 0;
}
