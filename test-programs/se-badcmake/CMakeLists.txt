cmake_minimum_required(VERSION 3.10)
project(demo)
# SE: configure 階段（cmake -G Ninja -B build）就故意失敗
message(FATAL_ERROR "intentional configure failure for SE test")
add_executable(main main.cpp)
