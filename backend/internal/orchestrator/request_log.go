package orchestrator

import (
	"context"

	"auto_performance/internal/domain"
	"auto_performance/internal/engine/k6"
)

// ifaceMeta 接口快照中最小的请求详情字段（接口 ID + 请求入参）。
type ifaceMeta struct {
	id   int64
	body string
}

// recordRequestLogs 把 k6 逐请求明细转为请求详情并批量写入（受日志设置控制）。
//
// 入参来源：
//   - k6 结果：方法/URL/状态码/错误码/错误信息/耗时/发生时间（每次请求一个点位）；
//   - 任务接口快照：接口 ID 兜底与请求入参（脚本以 interface_id 标签归属接口）。
//
// 写入失败只记测试日志，不影响任务结果与状态流转。
func (o *Orchestrator) recordRequestLogs(ctx context.Context, task *domain.TestTask, parsed *k6.Result) {
	if o == nil || o.requestLogs == nil || task == nil || parsed == nil {
		return
	}
	details := parsed.RequestDetails()
	if len(details) == 0 {
		return
	}

	byID, byURL := interfaceIndex(task)
	entries := make([]domain.RequestLog, 0, len(details))
	for i := range details {
		d := &details[i]
		entry := domain.RequestLog{
			TaskID:         task.ID,
			InterfaceID:    d.InterfaceID,
			OccurredAt:     d.Time,
			Method:         d.Method,
			URL:            d.URL,
			ResponseStatus: d.Status,
			ErrorCode:      d.ErrorCode,
			ErrorMessage:   d.ErrorMessage,
			DurationMs:     d.DurationMs,
		}
		meta, ok := byID[d.InterfaceID]
		if !ok {
			// 兼容历史脚本（无 interface_id 标签）：按完整 URL 回匹配接口快照。
			meta, ok = byURL[d.URL]
		}
		if ok {
			if entry.InterfaceID == 0 {
				entry.InterfaceID = meta.id
			}
			entry.RequestBody = meta.body
		}
		entries = append(entries, entry)
	}

	if err := o.requestLogs.WriteRequestLogs(ctx, task.ID, entries); err != nil {
		o.logBackground(task.ID, "warn", "写入请求详情失败（不影响任务结果）: "+err.Error())
	}
}

// interfaceIndex 构建接口快照索引：按接口 ID 与完整 URL（执行期拼接地址 + 快照展示地址）。
func interfaceIndex(task *domain.TestTask) (map[int64]ifaceMeta, map[string]ifaceMeta) {
	byID := make(map[int64]ifaceMeta, len(task.InterfaceSnapshots))
	byURL := make(map[string]ifaceMeta, len(task.InterfaceSnapshots))
	for _, snap := range task.InterfaceSnapshots {
		meta := ifaceMeta{id: snap.InterfaceID, body: snap.Body}
		if snap.InterfaceID > 0 {
			byID[snap.InterfaceID] = meta
		}
		if snap.URL != "" {
			byURL[snap.URL] = meta
		}
		if url, err := domain.InterfaceURL(snap.Scheme, task.TargetHost, snap.Port, snap.Path); err == nil && url != "" {
			byURL[url] = meta
		}
	}
	return byID, byURL
}
