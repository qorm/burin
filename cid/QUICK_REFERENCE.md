# CID 使用快速参考

## 基本使用

### 导入包
```go
import "burin/cid"
```

### 生成ID
```go
// 生成标准16字符ID
id := cid.Generate()  // 例: 0MHSVAI6U1F327VM

// 生成带前缀的ID
txID := "tx-" + cid.Generate()           // 事务ID
msgID := "msg-" + cid.Generate()         // 消息ID
sessionID := "sess-" + cid.Generate()    // 会话ID
```

### 验证ID
```go
// 验证ID是否有效
if cid.Validation(id) {
    fmt.Println("有效的CID")
}
```

### 提取时间戳
```go
// 从字符串ID提取时间戳（毫秒）
timestamp := cid.GetTimestampByStringID(id)
t := time.Unix(timestamp/1000, (timestamp%1000)*1e6)
```

## 项目中的ID类型

| 场景 | 格式 | 长度 | 示例 |
|------|------|------|------|
| 队列消息 | `{CID}` | 16 | `0MHSVAI6U1F327VM` |
| 客户端事务 | `tx-{CID}` | 19 | `tx-0MHSVAI6U1F327VM` |
| 服务端事务 | `txn_{NodeID}_{CID}` | 变长 | `txn_node1_0MHSVAI6U1F327VM` |
| 同步会话 | `sync_{NodeID}_{CID}` | 变长 | `sync_node1_0MHSVAI6U1F327VM` |

## CID格式说明

```
0MHSVAI6U1F327VM
├─────┬────┘└──┬──┘
│     │        └─ 后7位: 随机数 (0-ZZZZZZ)
│     └────────── 前9位: 时间戳 (毫秒级)
└──────────────── 36进制编码 (0-9, A-Z)
```

### 特性
- **字符集**: 0-9, A-Z（36进制）
- **长度**: 固定16字符
- **时间范围**: 支持约200年（从epoch开始）
- **随机性**: 后7位提供 ~78亿种组合
- **性能**: ~430 ns/op

## API参考

### 生成函数
```go
// 生成字符串ID（16字符）
func Generate() string

// 生成基于指定时间的字符串ID
func NewStringID(t time.Time) string

// 生成字节数组ID（10字节）
func NewBytesID(t time.Time) []byte
```

### 验证函数
```go
// 验证字符串ID是否有效
func Validation(cid string) bool
```

### 转换函数
```go
// 从字符串ID提取时间戳
func GetTimestampByStringID(stringID string) int64

// 从字节数组ID提取时间戳
func GetTimestampByBytesID(bytesID []byte) int64

// 字符串ID转字节数组ID
func StringIDToBytesID(stringID string) []byte

// 字节数组ID转字符串ID
func BytesIDToStringID(bytesID []byte) string
```

## 性能基准

```
BenchmarkCIDGeneration-10                2601188    475.4 ns/op    136 B/op    19 allocs/op
BenchmarkCIDGenerationParallel-10        2725802    430.3 ns/op    136 B/op    19 allocs/op
BenchmarkStringID-10                     2802601    428.1 ns/op    136 B/op    19 allocs/op
BenchmarkBytesID-10                     20569738     57.45 ns/op    40 B/op     3 allocs/op
```

## 最佳实践

### ✅ 推荐
```go
// 1. 使用Generate()生成新ID
id := cid.Generate()

// 2. 添加语义化前缀
txID := "tx-" + cid.Generate()

// 3. 验证外部输入的ID
if !cid.Validation(userProvidedID) {
    return errors.New("invalid ID format")
}

// 4. 在日志中使用ID便于追踪
log.Printf("Processing transaction %s", txID)
```

### ❌ 避免
```go
// 1. 不要自己拼接时间戳和随机数
id := fmt.Sprintf("%d-%d", time.Now().Unix(), rand.Int())  // ❌

// 2. 不要修改生成的ID
id = strings.Replace(cid.Generate(), "0", "O", -1)  // ❌

// 3. 不要假设ID的内部格式
parts := strings.Split(id, "-")  // ❌ ID内部没有分隔符

// 4. 不要用作加密密钥
key := []byte(cid.Generate())  // ❌ CID不是密码学安全的
```

## 并发安全

CID生成是并发安全的：
```go
// 可以在多个goroutine中并发调用
var wg sync.WaitGroup
for i := 0; i < 100; i++ {
    wg.Add(1)
    go func() {
        defer wg.Done()
        id := cid.Generate()  // 安全
        // 使用id...
    }()
}
wg.Wait()
```

## 数据库存储建议

### 字段定义
```sql
-- MySQL
id VARCHAR(32) NOT NULL,  -- 容纳前缀+CID

-- PostgreSQL
id TEXT NOT NULL,

-- 索引
CREATE INDEX idx_id ON table_name(id);
```

### 查询示例
```sql
-- 按时间范围查询（如果ID包含时间前缀）
SELECT * FROM messages 
WHERE id >= 'msg-0MHSV000000000'  -- 起始时间对应的CID
  AND id <= 'msg-0MHSVZZZZZZZ';   -- 结束时间对应的CID
```

## 故障排查

### Q: ID重复了怎么办？
A: 理论上不应该发生。如果发生，检查：
- 是否有多个进程使用相同的时间源
- 系统时间是否被回退
- 并发保护是否正常工作

### Q: ID长度不对
A: 确保：
- 使用 `cid.Generate()` 生成，不要手动构造
- 字符串没有被截断
- 数据库字段足够长

### Q: 性能问题
A: 如果ID生成成为瓶颈：
- 使用 `NewBytesID()` 代替字符串（快10倍）
- 考虑批量预生成ID
- 检查是否在热路径上过度生成ID

## 更多信息

- 完整文档: `/cid/OPTIMIZATION_SUMMARY.md`
- 集成说明: `/CID_INTEGRATION_SUMMARY.md`
- 测试用例: `/cid/*_test.go`
