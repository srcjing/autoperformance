package domain

import "testing"

// TestDefaultLogSetting 日志设置默认值（对齐 000006 迁移与 PRD §3.5）。
func TestDefaultLogSetting(t *testing.T) {
	s := DefaultLogSetting()
	if !s.Enabled {
		t.Fatal("默认应开启日志服务")
	}
	if s.MaxCount != DefaultLogMaxCount || s.RetentionDays != DefaultLogRetentionDays {
		t.Fatalf("默认条数/保留天数异常: %+v", s)
	}
	if !s.HasType(LogTypeRequest) || !s.HasType(LogTypeUser) {
		t.Fatalf("默认应启用全部日志类型: %v", s.LogTypes)
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("默认设置应合法: %v", err)
	}
}

// TestLogSettingNormalize 零值/未知类型归一化：零值填入默认，未知类型剔除。
func TestLogSettingNormalize(t *testing.T) {
	s := LogSetting{Enabled: false, LogTypes: []string{"request", "unknown", "request"}}
	got := s.Normalize()
	if got.MaxCount != DefaultLogMaxCount || got.RetentionDays != DefaultLogRetentionDays {
		t.Fatalf("零值未填默认: %+v", got)
	}
	if len(got.LogTypes) != 1 || got.LogTypes[0] != LogTypeRequest {
		t.Fatalf("日志类型应去重并剔除未知项: %v", got.LogTypes)
	}
	if got.Enabled {
		t.Fatal("enabled=false 不应被默认值覆盖")
	}
	if got.HasType(LogTypeUser) {
		t.Fatal("未勾选的类型不应视为启用")
	}
}

// TestLogSettingValidate 边界校验：条数/保留天数越界或启用时无类型 → 非法。
func TestLogSettingValidate(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*LogSetting)
		want bool
	}{
		{"条数过小", func(s *LogSetting) { s.MaxCount = MinLogMaxCount - 1 }, false},
		{"条数过大", func(s *LogSetting) { s.MaxCount = MaxLogMaxCount + 1 }, false},
		{"保留天数过小", func(s *LogSetting) { s.RetentionDays = 0 }, false},
		{"保留天数过大", func(s *LogSetting) { s.RetentionDays = MaxLogRetentionDays + 1 }, false},
		{"未知类型", func(s *LogSetting) { s.LogTypes = []string{"unknown"} }, false},
		{"启用但无类型", func(s *LogSetting) { s.LogTypes = nil }, false},
		{"关闭且无类型", func(s *LogSetting) { s.Enabled = false; s.LogTypes = nil }, true},
		{"合法", func(s *LogSetting) {}, true},
	}
	for _, c := range cases {
		s := DefaultLogSetting()
		c.mut(&s)
		if got := s.Validate() == nil; got != c.want {
			t.Errorf("%s: Validate 合法 = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestRequestLogHasContent 请求明细需至少包含 URL 或错误信息（空明细不落库）。
func TestRequestLogHasContent(t *testing.T) {
	if (RequestLog{}).HasContent() {
		t.Fatal("空明细不应视为有效")
	}
	if !(RequestLog{URL: "http://host/a"}).HasContent() {
		t.Fatal("仅 URL 也应视为有效明细")
	}
	if !(RequestLog{ErrorMessage: "connection refused"}).HasContent() {
		t.Fatal("仅错误信息也应视为有效明细")
	}
}
