package store

import (
	"container/list"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// TTLManager TTL管理器，使用时间轮算法
type TTLManager struct {
	wheels []*TimeWheel // 多级时间轮
	shard  *CacheShard
	logger *logrus.Logger
	stopCh chan struct{}
	doneCh chan struct{}
	mu     sync.RWMutex
}

// TimeWheel 时间轮
type TimeWheel struct {
	slots       []*list.List  // 时间槽
	slotCount   int           // 槽数量
	interval    time.Duration // 槽间隔
	currentSlot int           // 当前槽位置
	mu          sync.RWMutex
}

// TTLEntry TTL条目
type TTLEntry struct {
	key        string
	expireTime time.Time
	rounds     int // 需要等待的轮数
}

// NewTTLManager 创建TTL管理器
func NewTTLManager(shard *CacheShard, logger *logrus.Logger) *TTLManager {
	manager := &TTLManager{
		wheels: make([]*TimeWheel, 3), // 三级时间轮：秒、分、小时
		shard:  shard,
		logger: logger,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}

	// 初始化时间轮
	// 第一级：60个槽，每槽1秒 (0-59秒)
	manager.wheels[0] = NewTimeWheel(60, 1*time.Second)
	// 第二级：60个槽，每槽1分钟 (0-59分钟)
	manager.wheels[1] = NewTimeWheel(60, 1*time.Minute)
	// 第三级：24个槽，每槽1小时 (0-23小时)
	manager.wheels[2] = NewTimeWheel(24, 1*time.Hour)

	return manager
}

// NewTimeWheel 创建时间轮
func NewTimeWheel(slotCount int, interval time.Duration) *TimeWheel {
	tw := &TimeWheel{
		slots:       make([]*list.List, slotCount),
		slotCount:   slotCount,
		interval:    interval,
		currentSlot: 0,
	}

	for i := 0; i < slotCount; i++ {
		tw.slots[i] = list.New()
	}

	return tw
}

// Start 启动TTL管理器
func (m *TTLManager) Start() {
	go m.run()
}

// Stop 停止TTL管理器
func (m *TTLManager) Stop() {
	close(m.stopCh)
	<-m.doneCh
}

// AddKey 添加键到TTL管理
func (m *TTLManager) AddKey(key string, ttl time.Duration) {
	if ttl <= 0 {
		return
	}

	expireTime := time.Now().Add(ttl)
	entry := &TTLEntry{
		key:        key,
		expireTime: expireTime,
		rounds:     0,
	}

	// 添加到第一级时间轮
	m.addToWheel(entry, 0)
}

// RemoveKey 从TTL管理中移除键
func (m *TTLManager) RemoveKey(key string) {
	// 从所有时间轮中移除
	for _, wheel := range m.wheels {
		wheel.removeKey(key)
	}
}

// addToWheel 添加条目到指定级别的时间轮
func (m *TTLManager) addToWheel(entry *TTLEntry, level int) {
	if level >= len(m.wheels) {
		return
	}

	wheel := m.wheels[level]
	delay := time.Until(entry.expireTime)

	if delay <= 0 {
		// 已过期，直接删除
		go m.expireKey(entry.key)
		return
	}

	// 计算应该放入的槽位
	slots := int(delay / wheel.interval)
	if slots >= wheel.slotCount {
		// 超过当前级别的范围，放到下一级
		m.addToWheel(entry, level+1)
		return
	}

	wheel.mu.Lock()
	defer wheel.mu.Unlock()

	targetSlot := (wheel.currentSlot + slots) % wheel.slotCount
	entry.rounds = slots / wheel.slotCount

	wheel.slots[targetSlot].PushBack(entry)
}

// run 运行时间轮
func (m *TTLManager) run() {
	defer close(m.doneCh)

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.tick()
		}
	}
}

// tick 时间轮滴答
func (m *TTLManager) tick() {
	wheel := m.wheels[0] // 第一级时间轮
	wheel.mu.Lock()

	currentList := wheel.slots[wheel.currentSlot]
	wheel.currentSlot = (wheel.currentSlot + 1) % wheel.slotCount

	// 创建新的列表替换当前槽
	wheel.slots[(wheel.currentSlot-1+wheel.slotCount)%wheel.slotCount] = list.New()

	wheel.mu.Unlock()

	// 处理当前槽中的所有条目
	for e := currentList.Front(); e != nil; e = e.Next() {
		entry := e.Value.(*TTLEntry)

		if entry.rounds > 0 {
			entry.rounds--
			// 重新添加到时间轮
			m.addToWheel(entry, 0)
		} else {
			// 检查是否真的过期
			if time.Now().After(entry.expireTime) {
				go m.expireKey(entry.key)
			} else {
				// 重新添加
				m.addToWheel(entry, 0)
			}
		}
	}
}

// expireKey 过期键
func (m *TTLManager) expireKey(key string) {
	if err := m.shard.Delete(key); err != nil {
		m.logger.Errorf("Failed to delete expired key %s: %v", key, err)
	} else {
		m.logger.Debugf("Expired and deleted key: %s", key)
	}
}

// removeKey 从时间轮中移除键
func (tw *TimeWheel) removeKey(key string) {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	for _, slot := range tw.slots {
		var next *list.Element
		for e := slot.Front(); e != nil; e = next {
			next = e.Next()
			if entry, ok := e.Value.(*TTLEntry); ok && entry.key == key {
				slot.Remove(e)
			}
		}
	}
}
