package orchestrator

import (
	"context"
	"sync"
)

// runHandle 单次任务执行句柄（取消函数 + 完成信号）。
type runHandle struct {
	cancel context.CancelFunc
}

// registry 活跃执行注册表（按任务 ID），支撑 cancel 与优雅退出。
type registry struct {
	mu   sync.Mutex
	runs map[int64]*runHandle
}

// newRegistry 构造注册表。
func newRegistry() *registry {
	return &registry{runs: make(map[int64]*runHandle)}
}

// add 登记任务执行句柄。
func (r *registry) add(taskID int64, cancel context.CancelFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runs[taskID] = &runHandle{cancel: cancel}
}

// remove 移除任务执行句柄。
func (r *registry) remove(taskID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.runs, taskID)
}

// cancel 取消指定任务的执行（不存在返回 false）。
func (r *registry) cancel(taskID int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, ok := r.runs[taskID]
	if !ok {
		return false
	}
	h.cancel()
	delete(r.runs, taskID)
	return true
}

// cancelAll 取消全部执行（优雅退出）。
func (r *registry) cancelAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, h := range r.runs {
		h.cancel()
		delete(r.runs, id)
	}
}

// seqCounter 任务内测试日志序号（单调递增）。
type seqCounter struct {
	mu    sync.Mutex
	nexts map[int64]int64
}

// newSeqCounter 构造序号计数器。
func newSeqCounter() *seqCounter {
	return &seqCounter{nexts: make(map[int64]int64)}
}

// next 获取任务的下一个序号（从 1 开始）。
func (s *seqCounter) next(taskID int64) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nexts[taskID]++
	return s.nexts[taskID]
}
