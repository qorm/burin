package cid

import (
	"testing"
	"time"
)

func TestValidation(t *testing.T) {
	// 测试有效的CID
	validCID := Generate()
	if !Validation(validCID) {
		t.Errorf("Expected valid CID, got invalid: %s", validCID)
	}

	// 测试无效长度
	if Validation("short") {
		t.Error("Expected invalid for short string")
	}

	// 测试无效字符
	if Validation("123456789ABCDEFG") {
		t.Error("Expected invalid for string with invalid characters")
	}
}

func TestGenerateAndConvert(t *testing.T) {
	now := time.Now()

	// 测试生成
	stringID := NewStringID(now)
	if len(stringID) != 16 {
		t.Errorf("Expected length 16, got %d", len(stringID))
	}

	bytesID := NewBytesID(now)
	if len(bytesID) != 10 {
		t.Errorf("Expected length 10, got %d", len(bytesID))
	}

	// 测试转换
	convertedBytes := StringIDToBytesID(stringID)
	convertedString := BytesIDToStringID(bytesID)

	// 验证时间戳提取
	ts1 := GetTimestampByStringID(stringID)
	ts2 := GetTimestampByBytesID(bytesID)

	// 时间戳应该接近原始时间
	if abs(ts1-now.UnixMilli()) > 1 {
		t.Errorf("Timestamp mismatch for string ID: expected ~%d, got %d", now.UnixMilli(), ts1)
	}
	if abs(ts2-now.UnixMilli()) > 1 {
		t.Errorf("Timestamp mismatch for bytes ID: expected ~%d, got %d", now.UnixMilli(), ts2)
	}

	// 验证转换后的长度
	if len(convertedBytes) != 10 {
		t.Errorf("Converted bytes ID has wrong length: %d", len(convertedBytes))
	}
	if len(convertedString) != 16 {
		t.Errorf("Converted string ID has wrong length: %d", len(convertedString))
	}
}

func TestU36ToTenAndTenToU36(t *testing.T) {
	tests := []struct {
		decimal uint64
		base36  string
		length  int
	}{
		{0, "000000000", 9},
		{36, "000000010", 9},
		{1296, "000000100", 9},
		{46656, "000001000", 9},
	}

	for _, tt := range tests {
		// 测试十进制转36进制
		result := tenToU36(tt.decimal, tt.length)
		if result != tt.base36 {
			t.Errorf("tenToU36(%d, %d) = %s, want %s", tt.decimal, tt.length, result, tt.base36)
		}

		// 测试36进制转十进制
		decimal := u36ToTen(tt.base36)
		if decimal != tt.decimal {
			t.Errorf("u36ToTen(%s) = %d, want %d", tt.base36, decimal, tt.decimal)
		}
	}
}

func TestBytesToUint64AndUint64ToBytes(t *testing.T) {
	tests := []uint64{0, 1, 255, 256, 65535, 16777215, 4294967295}

	for _, original := range tests {
		bytes := Uint64ToBytes(original)
		if bytes == nil && original <= (1<<48-1) {
			t.Errorf("Uint64ToBytes(%d) returned nil", original)
			continue
		}
		if bytes != nil {
			result := bytesToUint64(bytes)
			if result != original {
				t.Errorf("Round trip failed: %d -> %v -> %d", original, bytes, result)
			}
		}
	}
}

func TestConcurrency(t *testing.T) {
	// 测试并发生成ID
	const goroutines = 100
	const idsPerGoroutine = 100

	done := make(chan bool, goroutines)
	ids := make(chan string, goroutines*idsPerGoroutine)

	for i := 0; i < goroutines; i++ {
		go func() {
			for j := 0; j < idsPerGoroutine; j++ {
				ids <- Generate()
			}
			done <- true
		}()
	}

	// 等待所有goroutine完成
	for i := 0; i < goroutines; i++ {
		<-done
	}
	close(ids)

	// 验证所有ID
	count := 0
	for id := range ids {
		if !Validation(id) {
			t.Errorf("Generated invalid ID: %s", id)
		}
		count++
	}

	if count != goroutines*idsPerGoroutine {
		t.Errorf("Expected %d IDs, got %d", goroutines*idsPerGoroutine, count)
	}
}

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

func BenchmarkBytesID(b *testing.B) {
	t := time.Now()
	b.ReportAllocs()
	b.SetBytes(32)
	for i := 0; i < b.N; i++ {
		_ = NewBytesID(t)
	}
}

func BenchmarkStringID(b *testing.B) {
	t := time.Now()
	b.ReportAllocs()
	b.SetBytes(32)
	for i := 0; i < b.N; i++ {
		_ = NewStringID(t)
	}
}
func BenchmarkStringIDToBytesID(b *testing.B) {
	id := NewStringID(time.Now())
	b.ReportAllocs()
	b.SetBytes(32)
	for i := 0; i < b.N; i++ {
		_ = StringIDToBytesID(id)
	}
}
func BenchmarkBytesIDToStringID(b *testing.B) {
	id := NewBytesID(time.Now())
	b.ReportAllocs()
	b.SetBytes(32)
	for i := 0; i < b.N; i++ {
		_ = BytesIDToStringID(id)
	}
}
func BenchmarkGetTimestampByStringID(b *testing.B) {
	id := NewStringID(time.Now())
	b.ReportAllocs()
	b.SetBytes(32)
	for i := 0; i < b.N; i++ {
		_ = GetTimestampByStringID(id)
	}
}
func BenchmarkGetTimestampByBytesID(b *testing.B) {
	id := NewBytesID(time.Now())
	b.ReportAllocs()
	b.SetBytes(32)
	for i := 0; i < b.N; i++ {
		_ = GetTimestampByBytesID(id)
	}
}

func TestMain(m *testing.M) {
	m.Run()
}
