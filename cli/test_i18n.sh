#!/bin/bash

# Burin CLI 多语言测试脚本

echo "================================"
echo "Burin CLI 多语言功能测试"
echo "================================"
echo ""

# 构建 CLI
echo "📦 构建 burin-cli..."
cd "$(dirname "$0")"
go build -o ../build/burin-cli-test .

if [ $? -ne 0 ]; then
    echo "❌ 构建失败"
    exit 1
fi

echo "✅ 构建成功"
echo ""

CLI="../build/burin-cli-test"

# 测试版本命令
echo "================================"
echo "测试 1: 版本命令"
echo "================================"
echo ""

echo "中文版本:"
$CLI --lang zh version
echo ""

echo "英文版本:"
$CLI --lang en version
echo ""

# 测试帮助命令
echo "================================"
echo "测试 2: 帮助信息"
echo "================================"
echo ""

echo "中文帮助:"
$CLI --lang zh --help | head -10
echo ""

echo "英文帮助:"
$CLI --lang en --help | head -10
echo ""

# 测试语言代码变体
echo "================================"
echo "测试 3: 语言代码变体"
echo "================================"
echo ""

echo "测试 zh_CN:"
$CLI --lang zh_CN version
echo ""

echo "测试 chinese:"
$CLI --lang chinese version
echo ""

echo "测试 en_US:"
$CLI --lang en_US version
echo ""

echo "测试 english:"
$CLI --lang english version
echo ""

# 测试环境变量
echo "================================"
echo "测试 4: 环境变量检测"
echo "================================"
echo ""

echo "LANG=zh_CN.UTF-8:"
LANG=zh_CN.UTF-8 $CLI version
echo ""

echo "LANG=en_US.UTF-8:"
LANG=en_US.UTF-8 $CLI version
echo ""

# 测试短参数
echo "================================"
echo "测试 5: 短参数 -l"
echo "================================"
echo ""

echo "使用 -l zh:"
$CLI -l zh version
echo ""

echo "使用 -l en:"
$CLI -l en version
echo ""

# 清理
echo "================================"
echo "清理临时文件..."
rm -f ../build/burin-cli-test
echo "✅ 清理完成"
echo ""

echo "================================"
echo "✅ 所有测试完成！"
echo "================================"
echo ""
echo "说明:"
echo "- 如果看到中英文输出不同,说明多语言功能正常工作"
echo "- 可以使用 --lang zh 或 --lang en 切换语言"
echo "- 支持的语言: zh/zh_CN/chinese (中文), en/en_US/english (英文)"
echo ""
