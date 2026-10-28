package log

import (
	"github.com/gin-gonic/gin"

	"auto_performance/internal/api/response"
	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
)

// ===================== 请求详情（GET /logs/request） =====================

// toRequestLogDTOs 请求详情视图 → 响应（ID 字符串化，时间统一 UTC RFC3339）。
func toRequestLogDTOs(list []domain.RequestLog) []requestLogDTO {
	out := make([]requestLogDTO, 0, len(list))
	for i := range list {
		entry := &list[i]
		out = append(out, requestLogDTO{
			ID:             itoa(entry.ID),
			TaskID:         idText(entry.TaskID),
			InterfaceID:    idText(entry.InterfaceID),
			Seq:            entry.Seq,
			OccurredAt:     formatTime(entry.OccurredAt),
			Method:         entry.Method,
			URL:            entry.URL,
			RequestBody:    entry.RequestBody,
			ResponseStatus: entry.ResponseStatus,
			ResponseBody:   entry.ResponseBody,
			ErrorCode:      entry.ErrorCode,
			ErrorMessage:   entry.ErrorMessage,
			DurationMs:     entry.DurationMs,
		})
	}
	return out
}

// idText 正数 ID → 十进制字符串（0 输出空串，前端按“未设置”处理）。
func idText(id int64) string {
	if id <= 0 {
		return ""
	}
	return itoa(id)
}

// ListRequestLogs GET /api/v1/logs/request（log.request:read）
//
// 筛选：task_id / interface_id / keyword（URL 或错误信息）/ start_time|end_time（25001）+ 分页。
// 带 task_id 时按 seq 升序（请求顺序），跨任务按发生时间倒序。
func (h *Handler) ListRequestLogs(c *gin.Context) {
	filters, e := parseListFilters(c)
	if e != nil {
		response.Error(c, e)
		return
	}
	taskID, e := parseIDValue(c.Query("task_id"))
	if e != nil {
		response.Error(c, e)
		return
	}
	interfaceID, e := parseIDValue(c.Query("interface_id"))
	if e != nil {
		response.Error(c, e)
		return
	}

	res, err := h.svc.ListRequestLogs(c.Request.Context(), domain.RequestLogQuery{
		LogQuery: domain.LogQuery{
			Keyword:   filters.Keyword,
			StartTime: filters.StartTime,
			EndTime:   filters.EndTime,
			Page:      filters.Page,
			PageSize:  filters.PageSize,
		},
		TaskID:      taskID,
		InterfaceID: interfaceID,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, gin.H{
		"list":      toRequestLogDTOs(res.List),
		"total":     res.Total,
		"page":      res.Page,
		"page_size": res.PageSize,
	})
}

// ===================== 日志设置（GET/PUT /log-settings） =====================

// toLogSettingDTO 日志设置视图 → 响应。
func toLogSettingDTO(setting *domain.LogSetting) logSettingDTO {
	out := logSettingDTO{
		Enabled:       setting.Enabled,
		MaxCount:      setting.MaxCount,
		RetentionDays: setting.RetentionDays,
		LogTypes:      append([]string(nil), setting.LogTypes...),
		UpdatedAt:     formatTime(setting.UpdatedAt),
	}
	if out.LogTypes == nil {
		out.LogTypes = []string{}
	}
	return out
}

// GetLogSetting GET /api/v1/log-settings（setting.log:write）
//
// 设置含写入与清理策略，属系统管理类配置：读写均要求可写权限，避免只读角色篡改留存策略。
func (h *Handler) GetLogSetting(c *gin.Context) {
	setting, err := h.svc.GetLogSetting(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, toLogSettingDTO(setting))
}

// UpdateLogSetting PUT /api/v1/log-settings（setting.log:write）
//
// 请求体仅绑定白名单字段；未提供的字段沿用当前设置（部分更新）。
// 保存成功后服务层立即按新策略清理一次（删过期/超量），校验失败 → 10003。
func (h *Handler) UpdateLogSetting(c *gin.Context) {
	var payload logSettingPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}

	current, err := h.svc.GetLogSetting(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	next := *current
	next.LogTypes = append([]string(nil), current.LogTypes...)
	if payload.Enabled != nil {
		next.Enabled = *payload.Enabled
	}
	if payload.MaxCount != nil {
		next.MaxCount = *payload.MaxCount
	}
	if payload.RetentionDays != nil {
		next.RetentionDays = *payload.RetentionDays
	}
	if payload.LogTypes != nil {
		next.LogTypes = append([]string(nil), (*payload.LogTypes)...)
	}

	saved, err := h.svc.UpdateLogSetting(c.Request.Context(), next)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, toLogSettingDTO(saved))
}
