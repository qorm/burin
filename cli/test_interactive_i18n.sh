#!/bin/bash

# 测试交互模式多语言功能

echo "================================"
echo "测试交互模式多语言"
echo "================================"
echo ""

CLI="../build/burin-cli-i18n"

echo "1. 测试中文帮助信息:"
$CLI --lang zh --help | head -5
echo ""

echo "2. 测试英文帮助信息:"
$CLI --lang en --help | head -5
echo ""

echo "3. 测试中文命令:"
$CLI --lang zh version
echo ""

echo "4. 测试英文命令:"
$CLI --lang en version
echo ""

echo "================================"
echo "交互模式测试说明:"
echo "================================"
echo ""
echo "要测试交互模式,请运行:"
echo ""
echo "# 中文交互模式"
echo "$CLI --lang zh interactive"
echo ""
echo "# 英文交互模式"
echo "$CLI --lang en interactive"
echo ""
echo "在交互模式中,所有提示和消息都应该使用所选语言。"
echo ""
