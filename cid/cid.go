package cid

// Package cid provides Correct Identifier generation and manipulation.
// CID is a time-based unique identifier with both string and binary representations.

import (
	crand "crypto/rand"
	"encoding/binary"
	"math/rand"
	"strings"
	"sync"
	"time"
)

const keyString string = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"

var (
	rng  *rand.Rand
	mu   sync.Mutex
	pool = sync.Pool{
		New: func() interface{} {
			return make([]byte, 4)
		},
	}
	// pow36 预计算36的幂次方，避免运行时重复计算，提升性能
	pow36 = [...]uint64{
		1, 36, 1296, 46656, 1679616, 60466176, 2176782336, 78364164096,
		2821109907456, 101559956668416, 3656158440062976,
	}
)

func init() {
	var n uint64
	binary.Read(crand.Reader, binary.LittleEndian, &n)
	rng = rand.New(rand.NewSource(time.Now().UnixNano() + int64(n)))
}

// Validation 验证给定的字符串是否为有效的CID
// CID必须是16个字符长度，且只包含0-9和A-Z字符
func Validation(cid string) bool {
	if len(cid) != 16 {
		return false
	}
	// 验证字符串只包含有效字符
	for i := 0; i < len(cid); i++ {
		if strings.IndexByte(keyString, cid[i]) < 0 {
			return false
		}
	}
	return u36ToTen(cid[9:]) < 4294967295
}

// Generate 生成一个基于当前时间的字符串CID
func Generate() string {
	return NewStringID(time.Now())
}

// NewBytesID 生成一个基于给定时间的字节数组CID（10字节）
func NewBytesID(t time.Time) []byte {
	b1 := Uint64ToBytes(uint64(t.UnixMilli()))
	b2 := randomPadding()
	return merge(b1[:6], b2[:4])
}

// NewStringID 生成一个基于给定时间的字符串CID（16字符）
func NewStringID(t time.Time) string {
	b1 := Uint64ToBytes(uint64(t.UnixMilli()))
	b2 := randomPadding()
	return b2s(b1[:6], b2[:4])
}

// GetTimestampByBytesID 从字节数组CID中提取时间戳
func GetTimestampByBytesID(bytesID []byte) int64 {
	return int64(bytesToUint64(bytesID[:6]))
}

// GetTimestampByStringID 从字符串CID中提取时间戳
func GetTimestampByStringID(stringID string) int64 {
	return int64(u36ToTen(stringID[:9]))
}

// StringIDToBytesID 将字符串CID转换为字节数组CID
func StringIDToBytesID(stringID string) []byte {
	t1 := u36ToTen(stringID[:9])
	t2 := u36ToTen(stringID[9:])
	b1 := Uint64ToBytes(t1)
	b2 := Uint64ToBytes(t2)
	return merge(b1[:6], b2[2:6])
}

// BytesIDToStringID 将字节数组CID转换为字符串CID
func BytesIDToStringID(bytesID []byte) string {
	return b2s(bytesID[:6], bytesID[6:10])
}

// b2s 将两个字节数组转换为36进制字符串
func b2s(b1, b2 []byte) string {
	i1 := uint64(b1[5]) | uint64(b1[4])<<8 | uint64(b1[3])<<16 |
		uint64(b1[2])<<24 | uint64(b1[1])<<32 | uint64(b1[0])<<40
	i2 := uint64(b2[3]) | uint64(b2[2])<<8 | uint64(b2[1])<<16 |
		uint64(b2[0])<<24
	return tenToU36(i1, 9) + tenToU36(i2, 7)
}

// randomPadding 生成4字节的随机填充，线程安全
func randomPadding() []byte {
	mu.Lock()
	defer mu.Unlock()

	// 使用对象池减少内存分配
	b := pool.Get().([]byte)
	n := rng.Uint32()
	binary.LittleEndian.PutUint32(b, n)

	result := make([]byte, 4)
	copy(result, b)
	pool.Put(b)

	return result
}

// merge 合并两个字节数组，高效实现
func merge(b1 []byte, b2 []byte) []byte {
	if len(b1) != 6 || len(b2) != 4 {
		return []byte{}
	}
	id := make([]byte, 10)
	copy(id[:6], b1)
	copy(id[6:], b2)
	return id
}

// tenToU36 将十进制数转换为36进制字符串，指定长度
func tenToU36(n uint64, u int) string {
	r := ""
	for n != 0 {
		r = string(keyString[n%36]) + r
		n = n / 36
	}
	for i := len(r); i < u; i++ {
		r = "0" + r
	}
	return r
}

// u36ToTen 将36进制字符串转换为十进制数，使用预计算的幂次方表提升性能
func u36ToTen(s string) uint64 {
	var n uint64
	l := len(s)
	for i := 0; i < l; i++ {
		r := strings.IndexByte(keyString, s[i])
		if r < 0 {
			return 0 // 无效字符
		}
		// 使用预计算的幂次方表或直接计算
		power := l - i - 1
		if power < len(pow36) {
			n += uint64(r) * pow36[power]
		} else {
			// 超出预计算范围时直接计算
			p := uint64(1)
			for j := 0; j < power; j++ {
				p *= 36
			}
			n += uint64(r) * p
		}
	}
	return n
}

// bytesToUint64 将字节数组转换为uint64，支持灵活长度
func bytesToUint64(b []byte) uint64 {
	if len(b) == 0 || len(b) > 6 {
		return 0
	}
	var result uint64
	for i := 0; i < len(b); i++ {
		result |= uint64(b[len(b)-1-i]) << (i * 8)
	}
	return result
}

// Uint64ToBytes 将uint64转换为6字节数组（大端序）
func Uint64ToBytes(u uint64) []byte {
	if u>>40 > 255 {
		return nil
	}
	b := make([]byte, 6)
	b[5] = byte(u)
	b[4] = byte(u >> 8)
	b[3] = byte(u >> 16)
	b[2] = byte(u >> 24)
	b[1] = byte(u >> 32)
	b[0] = byte(u >> 40)
	return b
}
