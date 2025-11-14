#!/bin/bash

# 测试Burin CLI多语言功能

echo "=========================================="
echo "Burin CLI 多语言功能演示"
echo "=========================================="
echo ""

CLI="./build/burin-cli-i18n"

echo "1. 中文帮助信息:"
echo "----------------------------------------"
$CLI --lang zh --help | head -15
echo ""

echo "2. 英文帮助信息:"
echo "----------------------------------------"
$CLI --lang en --help | head -15
echo ""

echo "3. 中文版本信息:"
echo "----------------------------------------"
$CLI --lang zh version
echo ""

echo "4. 英文版本信息:"
echo "----------------------------------------"
$CLI --lang en version
echo ""

echo "=========================================="
echo "多语言功能测试完成!"
echo ""
echo "使用说明:"
echo "  - 使用 --lang zh 或 -l zh 切换到中文"
echo "  - 使用 --lang en 或 -l en 切换到英文"
echo "  - 不指定语言时会自动检测系统语言(LANG环境变量)"
echo ""
echo "示例:"
echo "  $CLI --lang zh ping"
echo "  $CLI --lang en get mykey"
echo "  $CLI --lang zh interactive"
echo "=========================================="
