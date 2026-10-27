// Package domain 领域模型（实体 + 枚举/状态机）。
//
// 本文件定义测试管理实体（TestTemplate/TestTask）、任务快照、流程/报告/清理配置的
// 白名单校验，对齐 PRD §3.3/§4.7/§4.8/§9、api-contract §2.4 与不变量：
//   - INV-4：同环境同刻仅一个任务压测（环境行锁，服务层实现）；
//   - INV-5：任务创建时固化接口范围与阈值快照，历史报告可复现；
//   - INV-9：流程参数白名单校验（并发数正整数、时长白名单、URL 于引擎层二次校验）。
package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// 测试管理默认值与上限。
const (
	// DefaultMaxVUs 并发数上限默认值（配置项 k6.max_vus 覆盖）。
	DefaultMaxVUs = 10000
	// MaxInterfaceRange 单任务接口范围上限（保护快照体积与执行时长）。
	MaxInterfaceRange = 500
	// MaxRampStages 阶梯加压档位上限。
	MaxRampStages = 20
	// MaxCleanScripts 单次清理脚本数量上限。
	MaxCleanScripts = 10
	// MaxCleanScriptBytes 单条清理脚本内容上限（脚本内容需做哈希白名单校验）。
	MaxCleanScriptBytes = 64 * 1024
)

// 测试管理领域错误（服务层映射为 10003 / 23004）。
var (
	// ErrInvalidFlow 流程配置非法（结构/模式/时长/参数化）。
	ErrInvalidFlow = errors.New("测试流程配置非法")
	// ErrInvalidConcurrency 并发数非法或超出上限（→ 23004）。
	ErrInvalidConcurrency = errors.New("并发数非法或超出上限")
	// ErrInvalidReportConfig 报告配置非法（格式白名单）。
	ErrInvalidReportConfig = errors.New("报告配置非法")
	// ErrInvalidCleanConfig 清理配置非法（动作白名单/脚本路径）。
	ErrInvalidCleanConfig = errors.New("清理配置非法")
)

// ===================== 实体 =====================

// TestTemplate 测试模板（PRD §4.7）。
//
// Flow/ReportConfig/CleanConfig 为 JSONB 原文（白名单校验后归一化存储）；
// EnvIDs/InterfaceIDs 为默认选择集，基于模板创建任务时可由请求覆盖。
type TestTemplate struct {
	ID           int64
	Name         string
	EnvIDs       []int64
	InterfaceIDs []int64
	Flow         json.RawMessage
	ReportConfig json.RawMessage
	CleanConfig  json.RawMessage
	Description  string
	Version      int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// InterfaceSnapshot 接口范围快照项（创建任务时固化，INV-5）。
//
// 仅固化普通请求头与认证头名称；认证头明文不进入快照（INV-7），执行时由编排器
// 从接口记录解密读取。
type InterfaceSnapshot struct {
	InterfaceID     int64             `json:"interface_id"`
	ProductID       int64             `json:"product_id"`
	ProductName     string            `json:"product_name,omitempty"`
	Name            string            `json:"name"`
	Method          string            `json:"method"`
	URL             string            `json:"url"`
	Headers         map[string]string `json:"headers,omitempty"`
	AuthHeaderNames []string          `json:"auth_header_names,omitempty"`
	// AuthHeadersEnc 认证头 AES-256-GCM 密文（INV-7：不落明文；执行期由编排器解密注入脚本）。
	AuthHeadersEnc []byte            `json:"auth_headers_enc,omitempty"`
	Body           string            `json:"body,omitempty"`
	Timeouts       InterfaceTimeouts `json:"timeouts"`
	Status         string            `json:"status"`
	CapturedAt     time.Time         `json:"captured_at"`
}

// ThresholdSnapshot 阈值快照项（创建任务时固化，INV-2/INV-5）。
type ThresholdSnapshot struct {
	InterfaceID   int64   `json:"interface_id"`
	InterfaceName string  `json:"interface_name,omitempty"`
	Metric        string  `json:"metric"`
	Operator      string  `json:"operator"`
	Value         float64 `json:"value"`
	Unit          string  `json:"unit,omitempty"`
	Enabled       bool    `json:"enabled"`
}

// TestTask 测试任务（PRD §4.8）。
type TestTask struct {
	ID                 int64
	TemplateID         int64
	Name               string
	EnvID              int64
	CreatedBy          int64
	ProductIDs         []int64
	InterfaceIDs       []int64
	InterfaceSnapshots []InterfaceSnapshot
	ThresholdSnapshots []ThresholdSnapshot
	Flow               json.RawMessage
	ReportConfig       json.RawMessage
	CleanConfig        json.RawMessage
	Status             TaskStatus
	StartedAt          *time.Time
	FinishedAt         *time.Time
	Result             json.RawMessage
	Version            int64
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// TemplateQuery 模板列表查询条件。
type TemplateQuery struct {
	Keyword  string
	Page     int
	PageSize int
}

// TaskQuery 任务列表查询条件（关键字/状态/环境/产品/模板/时间范围）。
type TaskQuery struct {
	Keyword     string
	Status      TaskStatus
	EnvID       int64
	ProductID   int64
	TemplateID  int64
	CreatedFrom *time.Time
	CreatedTo   *time.Time
	Page        int
	PageSize    int
}

// ===================== 流程参数（白名单校验） =====================

// 并发模式枚举（PRD §3.3.6：固定并发 / 阶梯加压 / 脉冲）。
const (
	ConcurrencyModeFixed = "fixed"
	ConcurrencyModeRamp  = "ramp"
	ConcurrencyModePulse = "pulse"
)

// durationRE 时长白名单：数字（可带小数）+ 单位 ms/s/m/h，杜绝任意字符串进入脚本。
var durationRE = regexp.MustCompile(`^[0-9]{1,7}(\.[0-9]{1,3})?(ms|s|m|h)$`)

// numericRE 正整数白名单（并发数/档位 VU），拒绝 "1;exec('id')"、负数、小数与空值。
var numericRE = regexp.MustCompile(`^[0-9]{1,9}$`)

// safeParamPathRE 参数化数据文件相对路径白名单（禁止路径穿越/命令字符）。
var safeParamPathRE = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,255}$`)

// FlowConfig 流程参数（PRD §3.3.6，JSONB 原文结构）。
type FlowConfig struct {
	ConcurrencyMode  string            `json:"concurrency_mode"`
	TargetVUs        json.RawMessage   `json:"target_vus"`
	RampStages       []RampStage       `json:"ramp_stages"`
	Duration         string            `json:"duration"`
	Warmup           string            `json:"warmup"`
	Timeout          string            `json:"timeout"`
	ThinkTime        string            `json:"think_time"`
	Parameterization *Parameterization `json:"parameterization"`
	// 以下阶段配置为透传快照（准备/采集/报告/清理阶段开关与采样间隔）。
	Preparation json.RawMessage `json:"preparation"`
	Collection  json.RawMessage `json:"collection"`
	Report      json.RawMessage `json:"report"`
	Clean       json.RawMessage `json:"clean"`
}

// RampStage 阶梯加压档位（vus 同样走正整数白名单）。
type RampStage struct {
	VUs      json.RawMessage `json:"vus"`
	Duration string          `json:"duration"`
}

// Parameterization 参数化数据源。
type Parameterization struct {
	Source string `json:"source"` // csv | json
	Path   string `json:"path"`   // k6 工作目录内的相对路径
}

// RampStageValue 校验后的档位。
type RampStageValue struct {
	VUs      int
	Duration time.Duration
}

// FlowParams 校验并归一化后的流程参数（供 k6 脚本生成）。
type FlowParams struct {
	Mode             string
	VUs              int
	Stages           []RampStageValue
	Duration         time.Duration
	Warmup           time.Duration
	Timeout          time.Duration
	ThinkTime        time.Duration
	Parameterization *Parameterization

	// 原始字符串（秒级化后写入脚本，保持 k6 语法兼容）。
	DurationText  string
	WarmupText    string
	TimeoutText   string
	ThinkTimeText string
}

// ParseFlow 解析流程配置原文（空值 → ErrInvalidFlow）。
func ParseFlow(raw json.RawMessage) (*FlowConfig, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, fmt.Errorf("%w: 流程配置为空", ErrInvalidFlow)
	}
	var f FlowConfig
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidFlow, err)
	}
	return &f, nil
}

// Params 校验并归一化流程参数（INV-9 / DES-T-001）。
//
// 并发数必须为正整数且 ≤ maxVUs；时长走白名单正则；参数化路径禁止穿越。
func (f *FlowConfig) Params(maxVUs int) (FlowParams, error) {
	if maxVUs <= 0 {
		maxVUs = DefaultMaxVUs
	}
	mode := strings.TrimSpace(f.ConcurrencyMode)
	if mode == "" {
		mode = ConcurrencyModeFixed
	}
	switch mode {
	case ConcurrencyModeFixed, ConcurrencyModeRamp, ConcurrencyModePulse:
	default:
		return FlowParams{}, fmt.Errorf("%w: 并发模式不支持", ErrInvalidFlow)
	}

	p := FlowParams{Mode: mode, Parameterization: f.Parameterization}
	if p.Parameterization != nil {
		src := strings.ToLower(strings.TrimSpace(p.Parameterization.Source))
		path := strings.TrimSpace(p.Parameterization.Path)
		if src == "" && path == "" {
			p.Parameterization = nil
		} else {
			if src != "csv" && src != "json" {
				return FlowParams{}, fmt.Errorf("%w: 参数化来源仅支持 csv/json", ErrInvalidFlow)
			}
			if !safeParamPathRE.MatchString(path) || strings.Contains(path, "..") || strings.HasPrefix(path, "/") {
				return FlowParams{}, fmt.Errorf("%w: 参数化路径非法", ErrInvalidFlow)
			}
			p.Parameterization = &Parameterization{Source: src, Path: path}
		}
	}

	if mode == ConcurrencyModeRamp {
		if len(f.RampStages) == 0 {
			return FlowParams{}, fmt.Errorf("%w: 阶梯加压缺少档位", ErrInvalidFlow)
		}
		if len(f.RampStages) > MaxRampStages {
			return FlowParams{}, fmt.Errorf("%w: 档位数量超限", ErrInvalidFlow)
		}
		total := time.Duration(0)
		for _, st := range f.RampStages {
			vus, err := ParsePositiveInt(st.VUs)
			if err != nil || vus > maxVUs {
				return FlowParams{}, ErrInvalidConcurrency
			}
			d, err := ParseDuration(st.Duration)
			if err != nil {
				return FlowParams{}, err
			}
			total += d
			p.Stages = append(p.Stages, RampStageValue{VUs: vus, Duration: d})
			if vus > p.VUs {
				p.VUs = vus
			}
		}
		if total <= 0 || total > 24*time.Hour {
			return FlowParams{}, fmt.Errorf("%w: 总时长非法", ErrInvalidFlow)
		}
		p.Duration = total
		p.DurationText = total.String()
	} else {
		vus, err := ParsePositiveInt(f.TargetVUs)
		if err != nil || vus > maxVUs {
			return FlowParams{}, ErrInvalidConcurrency
		}
		d, err := ParseDuration(f.Duration)
		if err != nil {
			return FlowParams{}, err
		}
		p.VUs = vus
		p.Duration = d
		p.DurationText = d.String()
	}

	if warmup := strings.TrimSpace(f.Warmup); warmup != "" {
		d, err := ParseDuration(warmup)
		if err != nil {
			return FlowParams{}, fmt.Errorf("%w: 预热时长非法", ErrInvalidFlow)
		}
		if d > time.Hour || d >= p.Duration {
			return FlowParams{}, fmt.Errorf("%w: 预热时长超出范围", ErrInvalidFlow)
		}
		p.Warmup, p.WarmupText = d, d.String()
	}
	if timeout := strings.TrimSpace(f.Timeout); timeout != "" {
		d, err := ParseDuration(timeout)
		if err != nil || d <= 0 || d > 5*time.Minute {
			return FlowParams{}, fmt.Errorf("%w: 请求超时非法", ErrInvalidFlow)
		}
		p.Timeout, p.TimeoutText = d, d.String()
	}
	if think := strings.TrimSpace(f.ThinkTime); think != "" {
		d, err := ParseDuration(think)
		if err != nil || d > 5*time.Minute {
			return FlowParams{}, fmt.Errorf("%w: 思考时间非法", ErrInvalidFlow)
		}
		p.ThinkTime, p.ThinkTimeText = d, d.String()
	}
	return p, nil
}

// ParsePositiveInt 解析正整数（接受 JSON 数字或纯数字字符串），拒绝注入型取值。
func ParsePositiveInt(raw json.RawMessage) (int, error) {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return 0, ErrInvalidConcurrency
	}
	s = strings.Trim(s, `"`)
	if !numericRE.MatchString(s) {
		return 0, ErrInvalidConcurrency
	}
	v, err := strconv.Atoi(s)
	if err != nil || v <= 0 {
		return 0, ErrInvalidConcurrency
	}
	return v, nil
}

// ParseDuration 解析时长白名单（1ms ~ 24h）。
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if !durationRE.MatchString(s) {
		return 0, fmt.Errorf("%w: 时长格式非法", ErrInvalidFlow)
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 || d > 24*time.Hour {
		return 0, fmt.Errorf("%w: 时长超出范围", ErrInvalidFlow)
	}
	return d, nil
}

// ===================== 报告配置 =====================

// 报告格式枚举（PRD §4.9 / 决策 #4：默认全量 html+pdf+word）。
const (
	ReportFormatHTML = "html"
	ReportFormatPDF  = "pdf"
	ReportFormatWord = "word"
)

// ReportConfig 报告输出配置。
type ReportConfig struct {
	Formats []string `json:"formats"`
}

// ReportFormats 全部报告格式（默认全量）。
func ReportFormats() []string {
	return []string{ReportFormatHTML, ReportFormatPDF, ReportFormatWord}
}

// ParseReportConfig 校验报告配置：缺省 formats 时按全量生成，显式给出必须为非空白名单子集。
func ParseReportConfig(raw json.RawMessage) (json.RawMessage, *ReportConfig, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil, fmt.Errorf("%w: 报告配置为空", ErrInvalidReportConfig)
	}
	var cfg ReportConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrInvalidReportConfig, err)
	}
	if len(cfg.Formats) == 0 {
		cfg.Formats = ReportFormats()
	}
	seen := make(map[string]bool, len(cfg.Formats))
	out := make([]string, 0, len(cfg.Formats))
	for _, f := range cfg.Formats {
		v := strings.ToLower(strings.TrimSpace(f))
		switch v {
		case ReportFormatHTML, ReportFormatPDF, ReportFormatWord:
		default:
			return nil, nil, fmt.Errorf("%w: 不支持的报告格式 %q", ErrInvalidReportConfig, f)
		}
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil, nil, fmt.Errorf("%w: 报告格式为空", ErrInvalidReportConfig)
	}
	cfg.Formats = out
	normalized, err := json.Marshal(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrInvalidReportConfig, err)
	}
	return normalized, &cfg, nil
}

// ===================== 清理配置 =====================

// 清理动作枚举（PRD §3.3.7 / 决策 #15）。
const (
	CleanActionRestoreConfig   = "restore_config"
	CleanActionCleanData       = "clean_data"
	CleanActionRestartService  = "restart_service"
	CleanActionReleaseResource = "release_resource"
	CleanActionScript          = "script"
)

// 清理执行时机（决定成功自动/失败保留现场之外的显式覆盖）。
const (
	CleanTimingAuto   = "auto"
	CleanTimingManual = "manual"
)

// CleanActions 全部内置清理动作。
func CleanActions() []string {
	return []string{
		CleanActionRestoreConfig, CleanActionCleanData,
		CleanActionRestartService, CleanActionReleaseResource,
	}
}

// CleanScript 用户清理脚本（DES-T-003：仅在白名单主机执行，且需内容哈希白名单）。
type CleanScript struct {
	HostID  FlexID `json:"host_id"`
	Path    string `json:"path"`
	SHA256  string `json:"sha256,omitempty"`
	Content string `json:"content,omitempty"`
}

// CleanConfig 清理配置（动作列表 + 用户脚本 + 执行时机）。
type CleanConfig struct {
	Actions []string      `json:"actions"`
	Scripts []CleanScript `json:"scripts,omitempty"`
	Timing  string        `json:"timing,omitempty"`
}

// ParseCleanConfig 校验清理配置：动作必须在白名单内，脚本路径禁止穿越与命令字符。
func ParseCleanConfig(raw json.RawMessage) (json.RawMessage, *CleanConfig, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil, fmt.Errorf("%w: 清理配置为空", ErrInvalidCleanConfig)
	}
	var cfg CleanConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrInvalidCleanConfig, err)
	}
	actions := make([]string, 0, len(cfg.Actions))
	seen := make(map[string]bool, len(cfg.Actions))
	for _, a := range cfg.Actions {
		v := strings.ToLower(strings.TrimSpace(a))
		switch v {
		case CleanActionRestoreConfig, CleanActionCleanData, CleanActionRestartService,
			CleanActionReleaseResource, CleanActionScript:
		default:
			return nil, nil, fmt.Errorf("%w: 不支持的清理动作 %q", ErrInvalidCleanConfig, a)
		}
		if seen[v] {
			continue
		}
		seen[v] = true
		actions = append(actions, v)
	}
	cfg.Actions = actions

	if len(cfg.Scripts) > MaxCleanScripts {
		return nil, nil, fmt.Errorf("%w: 清理脚本数量超限", ErrInvalidCleanConfig)
	}
	for i := range cfg.Scripts {
		s := &cfg.Scripts[i]
		if s.HostID <= 0 {
			return nil, nil, fmt.Errorf("%w: 清理脚本缺少主机", ErrInvalidCleanConfig)
		}
		if !safeParamPathRE.MatchString(strings.TrimSpace(s.Path)) || strings.Contains(s.Path, "..") {
			return nil, nil, fmt.Errorf("%w: 清理脚本路径非法", ErrInvalidCleanConfig)
		}
		if len(s.Content) > MaxCleanScriptBytes {
			return nil, nil, fmt.Errorf("%w: 清理脚本内容过大", ErrInvalidCleanConfig)
		}
		if s.SHA256 != "" && !sha256RE.MatchString(strings.ToLower(s.SHA256)) {
			return nil, nil, fmt.Errorf("%w: 清理脚本哈希非法", ErrInvalidCleanConfig)
		}
	}
	if len(actions) == 0 && len(cfg.Scripts) == 0 {
		return nil, nil, fmt.Errorf("%w: 清理动作与脚本均为空", ErrInvalidCleanConfig)
	}

	timing := strings.ToLower(strings.TrimSpace(cfg.Timing))
	if timing == "" {
		timing = CleanTimingAuto
	}
	if timing != CleanTimingAuto && timing != CleanTimingManual {
		return nil, nil, fmt.Errorf("%w: 清理时机非法", ErrInvalidCleanConfig)
	}
	cfg.Timing = timing

	normalized, err := json.Marshal(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrInvalidCleanConfig, err)
	}
	return normalized, &cfg, nil
}

// sha256RE 十六进制 SHA256（64 位）。
var sha256RE = regexp.MustCompile(`^[a-f0-9]{64}$`)

// FlexID 兼容字符串/数字形式的 bigint ID（前端表单常以字符串传递 ID）。
type FlexID int64

// UnmarshalJSON 接受 "12" / 12 / null。
func (f *FlexID) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "null" || raw == `""` {
		*f = 0
		return nil
	}
	raw = strings.Trim(raw, `"`)
	if !numericRE.MatchString(raw) {
		return fmt.Errorf("id 取值非法: %s", string(data))
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v < 0 {
		return fmt.Errorf("id 取值非法: %s", string(data))
	}
	*f = FlexID(v)
	return nil
}

// Int64 返回底层值。
func (f FlexID) Int64() int64 { return int64(f) }
