#include <iostream>
// CE: `>> b` 之後故意漏掉分號，編譯階段會失敗
int main() {
    int a, b;
    std::cin >> a >> b
    std::cout << a + b << "\n";
    return 0;
}
