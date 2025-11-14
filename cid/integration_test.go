package cid_test

import (
	"testing"

	"burin/cid"
)

// TestCIDIntegration 测试CID在整个项目中的集成
func TestCIDIntegration(t *testing.T) {
	// 测试生成大量ID的唯一性
	const count = 10000
	ids := make(map[string]bool, count)

	for i := 0; i < count; i++ {
		id := cid.Generate()

		// 验证ID格式
		if !cid.Validation(id) {
			t.Errorf("Generated invalid ID: %s", id)
		}

		// 验证ID唯一性
		if ids[id] {
			t.Errorf("Duplicate ID generated: %s", id)
		}
		ids[id] = true
	}

	t.Logf("Successfully generated %d unique IDs", count)
}

// TestCIDWithPrefix 测试带前缀的ID生成（用于不同类型的ID）
func TestCIDWithPrefix(t *testing.T) {
	testCases := []struct {
		name   string
		prefix string
	}{
		{"Transaction ID", "tx-"},
		{"Message ID", "msg-"},
		{"Session ID", "sess-"},
		{"Sync ID", "sync_node1_"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			id := tc.prefix + cid.Generate()

			// 验证前缀
			if len(id) < len(tc.prefix) {
				t.Errorf("ID too short: %s", id)
			}

			// 提取CID部分并验证
			cidPart := id[len(tc.prefix):]
			if !cid.Validation(cidPart) {
				t.Errorf("Invalid CID part in %s: %s", id, cidPart)
			}

			t.Logf("Generated %s: %s", tc.name, id)
		})
	}
}

// BenchmarkCIDGeneration 基准测试CID生成性能
func BenchmarkCIDGeneration(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = cid.Generate()
	}
}

// BenchmarkCIDGenerationParallel 并发基准测试
func BenchmarkCIDGenerationParallel(b *testing.B) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = cid.Generate()
		}
	})
}
