package orchestrator

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"auto_performance/internal/domain"
	"auto_performance/internal/engine/k6"
)

const (
	// afreqPrefix k6 脚本采集响应体时输出的行前缀（见 engine/k6/script.go）。
	afreqPrefix = "AFREQ|"
	// afwarnPrefix 流程变量/提取异常告警行前缀（服务端转写测试日志）。
	afwarnPrefix = "AFWARN|"
	// maxFlowWarnings 单任务写入测试日志的步骤告警上限（防刷屏）。
	maxFlowWarnings = 100
	// maxCapturedRequests 单任务从 stdout 收集的明细上限（防内存/日志爆炸）。
	maxCapturedRequests = 5000
)

// capturedRequest k6 脚本输出的单请求明细（含响应体）。
// 注：k6 侧 interfaceId 为字符串，故用 string 接收再转 int64（避免 Unmarshal 失败）。
type capturedRequest struct {
	InterfaceID string `json:"i"`
	Method      string `json:"m"`
	URL         string `json:"u"`
	Status      int    `json:"s"`
	DurationMs  int64  `json:"d"`
	Error       string `json:"e"`
	// RequestBody 实际发送的请求体（渲染后，≤4KB）。
	RequestBody string `json:"rb"`
	Body        string `json:"b"`
}

// parseCapturedRequest 解析 `AFREQ|{json}` 行。
func parseCapturedRequest(line string) (capturedRequest, bool) {
	var cr capturedRequest
	if !strings.HasPrefix(line, afreqPrefix) {
		return cr, false
	}
	if err := json.Unmarshal([]byte(line[len(afreqPrefix):]), &cr); err != nil {
		return cr, false
	}
	return cr, true
}

// flowWarning k6 脚本输出的步骤告警（变量未定义 / 提取失败）。
type flowWarning struct {
	Key     string `json:"k"`
	Message string `json:"m"`
}

// parseFlowWarning 解析 AFWARN 行。
func parseFlowWarning(line string) (flowWarning, bool) {
	var fw flowWarning
	if !strings.HasPrefix(line, afwarnPrefix) {
		return fw, false
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(line[len(afwarnPrefix):])), &fw); err != nil {
		return fw, false
	}
	return fw, strings.TrimSpace(fw.Message) != ""
}

// ifaceMeta 接口快照中最小的请求详情字段（接口 ID + 请求入参）。
type ifaceMeta struct {
	id   int64
	body string
}

// recordRequestLogs 把逐请求明细转为请求详情并批量写入（受日志设置控制）。
//
// 明细来源优先级：
//   - captured：k6 脚本在「采集响应体」开启时输出的 AFREQ 行（含响应体，最完整）；
//   - 否则回退 parsed.RequestDetails()（k6 result.json 点位还原，无响应体）。
//
// 写入失败只记测试日志，不影响任务结果与状态流转。
func (o *Orchestrator) recordRequestLogs(ctx context.Context, task *domain.TestTask, parsed *k6.Result, captured []capturedRequest) {
	if o == nil || o.requestLogs == nil || task == nil {
		return
	}
	byID, byURL := interfaceIndex(task)
	entries := make([]domain.RequestLog, 0)

	if len(captured) > 0 {
		now := o.now()
		for i := range captured {
			c := &captured[i]
			var ifaceID int64
			if c.InterfaceID != "" {
				ifaceID, _ = strconv.ParseInt(c.InterfaceID, 10, 64)
			}
			entry := domain.RequestLog{
				TaskID:         task.ID,
				InterfaceID:    ifaceID,
				OccurredAt:     now,
				Method:         c.Method,
				URL:            c.URL,
				RequestBody:    c.RequestBody,
				ResponseStatus: c.Status,
				ErrorMessage:   c.Error,
				DurationMs:     c.DurationMs,
				ResponseBody:   c.Body,
			}
			meta, ok := byID[ifaceID]
			if !ok {
				meta, ok = byURL[c.URL]
			}
			if ok {
				if entry.InterfaceID == 0 {
					entry.InterfaceID = meta.id
				}
				// 脚本未回传实际请求体时（旧脚本/异常）回退接口或步骤配置的请求体。
				if entry.RequestBody == "" {
					entry.RequestBody = meta.body
				}
			}
			entries = append(entries, entry)
		}
	} else {
		if parsed == nil {
			return
		}
		details := parsed.RequestDetails()
		if len(details) == 0 {
			return
		}
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
	}

	if len(entries) == 0 {
		return
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
	// 流程模式下步骤级请求体覆盖优先（未开响应体采集时也能看到实际请求入参）。
	if flow, err := domain.ParseFlow(task.Flow); err == nil && flow.TestModeValue() == domain.TestModeFlow {
		if steps, err := flow.ValidateSteps(); err == nil {
			for _, step := range steps {
				if strings.TrimSpace(step.Body) == "" {
					continue
				}
				if meta, ok := byID[step.InterfaceID]; ok {
					meta.body = step.Body
					byID[step.InterfaceID] = meta
				}
			}
		}
	}
	return byID, byURL
}

// 引用 time 以保证 OccurredAt 语义（now 由 o.now() 提供）。
var _ = time.Now
