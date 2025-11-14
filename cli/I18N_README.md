# Burin CLI 多语言支持

## 概述

Burin CLI 现已支持多语言界面,目前支持:
- 中文 (zh_CN) - 默认语言
- 英文 (en_US)

## 使用方法

### 1. 自动检测系统语言

不指定语言参数时,CLI 会自动从系统环境变量 `LANG` 中检测语言:

```bash
# 系统语言为中文时
./burin-cli ping

# 系统语言为英文时
LANG=en_US.UTF-8 ./burin-cli ping
```

### 2. 手动指定语言

使用 `--lang` 或 `-l` 参数指定语言:

```bash
# 使用中文
./burin-cli --lang zh ping
./burin-cli --lang zh_CN ping
./burin-cli --lang chinese ping
./burin-cli -l zh ping

# 使用英文
./burin-cli --lang en ping
./burin-cli --lang en_US ping
./burin-cli --lang english ping
./burin-cli -l en ping
```

### 3. 所有命令都支持多语言

```bash
# 中文命令示例
./burin-cli --lang zh version
./burin-cli --lang zh get mykey
./burin-cli --lang zh set mykey myvalue
./burin-cli --lang zh list
./burin-cli --lang zh cluster
./burin-cli --lang zh health

# 英文命令示例
./burin-cli --lang en version
./burin-cli --lang en get mykey
./burin-cli --lang en set mykey myvalue
./burin-cli --lang en list
./burin-cli --lang en cluster
./burin-cli --lang en health
```

### 4. 交互模式

交互模式也支持多语言:

```bash
# 中文交互模式
./burin-cli --lang zh interactive

# 英文交互模式
./burin-cli --lang en interactive
```

## 支持的语言代码

### 中文
- `zh`
- `zh_CN`
- `zh-cn`
- `chinese`
- `cn`

### 英文
- `en`
- `en_US`
- `en-us`
- `english`
- `us`

## 示例输出对比

### 中文输出
```bash
$ ./burin-cli --lang zh ping
✅ PONG (耗时: 2.5ms)

$ ./burin-cli --lang zh get mykey
myvalue
(耗时: 1.2ms)

$ ./burin-cli --lang zh list
1) key1
2) key2
3) key3

总计: 3 个键 (耗时: 3.1ms)
```

### 英文输出
```bash
$ ./burin-cli --lang en ping
✅ PONG (Time: 2.5ms)

$ ./burin-cli --lang en get mykey
myvalue
(Time: 1.2ms)

$ ./burin-cli --lang en list
1) key1
2) key2
3) key3

Total: 3 keys (Time: 3.1ms)
```

## 环境变量

可以通过设置 `LANG` 环境变量来设置默认语言:

```bash
# 临时设置为英文
export LANG=en_US.UTF-8
./burin-cli ping

# 临时设置为中文
export LANG=zh_CN.UTF-8
./burin-cli ping
```

## 添加新语言

如果需要添加新的语言支持,请按以下步骤操作:

1. 在 `cli/i18n/` 目录下创建新的语言文件,例如 `ja_JP.go` (日语)
2. 实现 `registerJaJP()` 函数,提供所有消息的翻译
3. 在 `cli/i18n/i18n.go` 的 `Init()` 函数中调用新的注册函数
4. 在 `SetLanguage()` 函数中添加新语言的代码映射

## 技术实现

多语言支持通过 `cli/i18n` 包实现:

- `i18n.go` - 核心翻译引擎和语言管理
- `zh_CN.go` - 中文翻译
- `en_US.go` - 英文翻译

所有用户可见的消息都通过 `i18n.T()` 函数进行翻译,确保一致的多语言体验。

## 构建和测试

```bash
# 构建 CLI
cd cli
go build -o ../build/burin-cli main.go

# 测试中文
../build/burin-cli --lang zh version

# 测试英文
../build/burin-cli --lang en version

# 测试自动检测
LANG=zh_CN.UTF-8 ../build/burin-cli version
LANG=en_US.UTF-8 ../build/burin-cli version
```

## 注意事项

1. 语言参数对所有命令和子命令都生效
2. 如果指定的语言不支持,会自动回退到中文
3. 错误消息和帮助信息也会根据选择的语言显示
4. 交互模式下的所有提示和输出都使用选定的语言
