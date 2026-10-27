// Package cleaner 环境清理器。
//
// 对齐 PRD §3.3.7 / 决策 #15/#16 与 DES-T-003：
//   - 内置动作（恢复配置/清理数据/重启服务/释放资源）执行固定的受控脚本；
//   - 用户脚本仅允许在白名单 SSH 主机上执行，且脚本内容/声明哈希必须命中哈希白名单；
//   - 任务成功自动清理（cleaning → finished），失败/取消保留现场待人工确认。
package cleaner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"auto_performance/internal/domain"
)

// maxActionSteps 单次清理最多执行的动作/脚本数量（防止配置放大）。
const maxActionSteps = 32

// cleanupHelperPath 受控清理助手（安装包部署到每台可信主机，按动作参数执行）。
const cleanupHelperPath = "/usr/local/bin/auto-performance-cleanup"

// Action 内置清理动作。
type Action = string

// 内置动作别名（与 domain.CleanAction* 保持一致）。
const (
	ActionRestoreConfig   = domain.CleanActionRestoreConfig
	ActionCleanData       = domain.CleanActionCleanData
	ActionRestartService  = domain.CleanActionRestartService
	ActionReleaseResource = domain.CleanActionReleaseResource
	ActionScript          = domain.CleanActionScript
)

// builtinActions 动作 → 受控助手参数（固定集，参数不来自用户文本）。
var builtinActions = map[Action]string{
	ActionRestoreConfig:   "restore-config",
	ActionCleanData:       "clean-data",
	ActionRestartService:  "restart-service",
	ActionReleaseResource: "release-resource",
}

// 清理领域错误（服务层按依赖异常/参数错误映射）。
var (
	// ErrInvalidPlan 清理计划非法。
	ErrInvalidPlan = errors.New("清理计划非法")
	// ErrNoHost 未配置清理主机（环境未绑定 SSH/Host）。
	ErrNoHost = errors.New("清理主机未配置")
	// ErrHostNotAllowlisted 清理主机不在白名单内（DES-T-003）。
	ErrHostNotAllowlisted = errors.New("清理主机未在白名单内")
	// ErrScriptNotAllowlisted 清理脚本哈希不在白名单内（DES-T-003）。
	ErrScriptNotAllowlisted = errors.New("清理脚本未在白名单内")
	// ErrNoRunner 清理执行器未就绪（SSH 模块未配置）。
	ErrNoRunner = errors.New("清理执行器未就绪")
)

// scriptPathRE 远程脚本路径白名单（绝对路径、无穿越、无命令元字符）。
var scriptPathRE = regexp.MustCompile(`^(/[A-Za-z0-9._-]+)+$`)

// Script 用户清理脚本。
type Script struct {
	HostID  int64
	Path    string
	SHA256  string
	Content string
}

// Plan 清理计划（动作 + 脚本 + 目标主机）。
type Plan struct {
	Actions []Action
	Scripts []Script
	Hosts   []int64
}

// Runner 远程命令执行能力（由 SSHHostRunner 实现；测试可注入假实现）。
type Runner interface {
	// Run 在指定主机执行固定命令（command 为受控路径，args 为白名单枚举）。
	Run(ctx context.Context, hostID int64, command string, args []string) (string, error)
}

// Allowlist 可信主机/脚本哈希白名单（由系统设置 SSH/Host 与运维审批清单加载）。
type Allowlist struct {
	HostIDs      map[int64]bool
	ScriptHashes map[string]bool
}

// NewAllowlist 构造白名单（哈希统一转小写）。
func NewAllowlist(hostIDs []int64, scriptHashes []string) Allowlist {
	hosts := make(map[int64]bool, len(hostIDs))
	for _, id := range hostIDs {
		if id > 0 {
			hosts[id] = true
		}
	}
	hashes := make(map[string]bool, len(scriptHashes))
	for _, h := range scriptHashes {
		v := strings.ToLower(strings.TrimSpace(h))
		if v != "" {
			hashes[v] = true
		}
	}
	return Allowlist{HostIDs: hosts, ScriptHashes: hashes}
}

// StepResult 单步清理结果。
type StepResult struct {
	Action Action
	HostID int64
	Path   string
	Output string
	Err    error
}

// Result 清理结果（逐步记录，异常不中断其余步骤以尽量恢复现场）。
type Result struct {
	Steps []StepResult
}

// OK 是否全部步骤成功。
func (r Result) OK() bool {
	for _, s := range r.Steps {
		if s.Err != nil {
			return false
		}
	}
	return true
}

// Cleaner 环境清理器。
type Cleaner struct {
	runner  Runner
	allow   Allowlist
	timeout time.Duration
}

// New 构造清理器；runner 为 nil 时 Run 返回 ErrNoRunner（SSH 模块未就绪）。
func New(runner Runner, allow Allowlist) *Cleaner {
	return &Cleaner{runner: runner, allow: allow, timeout: 2 * time.Minute}
}

// ParsePlan 由清理配置原文与目标主机构造计划（动作白名单 + 脚本字段校验）。
func ParsePlan(raw json.RawMessage, hosts []int64) (*Plan, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, fmt.Errorf("%w: 清理配置为空", ErrInvalidPlan)
	}
	var cfg domain.CleanConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidPlan, err)
	}

	plan := &Plan{Hosts: dedupeHosts(hosts)}
	for _, a := range cfg.Actions {
		action := strings.ToLower(strings.TrimSpace(a))
		if _, ok := builtinActions[action]; !ok && action != ActionScript {
			return nil, fmt.Errorf("%w: 不支持的动作 %q", ErrInvalidPlan, a)
		}
		if action == ActionScript {
			// script 动作必须伴随显式脚本项，避免「空动作」静默跳过清理。
			if len(cfg.Scripts) == 0 {
				return nil, fmt.Errorf("%w: script 动作缺少脚本", ErrInvalidPlan)
			}
			continue
		}
		plan.Actions = append(plan.Actions, action)
	}
	for _, s := range cfg.Scripts {
		path := strings.TrimSpace(s.Path)
		if !validScriptPath(path) {
			return nil, fmt.Errorf("%w: 脚本路径非法", ErrInvalidPlan)
		}
		plan.Scripts = append(plan.Scripts, Script{
			HostID:  s.HostID.Int64(),
			Path:    path,
			SHA256:  strings.ToLower(strings.TrimSpace(s.SHA256)),
			Content: s.Content,
		})
	}
	if len(plan.Actions) == 0 && len(plan.Scripts) == 0 {
		// 未配置任何清理动作/脚本：合法（无操作，表示不清理；接口压测默认场景）。
		return plan, nil
	}
	if len(plan.Actions)+len(plan.Scripts) > maxActionSteps {
		return nil, fmt.Errorf("%w: 清理步骤超限", ErrInvalidPlan)
	}
	if len(plan.Hosts) == 0 {
		// 无清理主机：接口压测不修改本地环境，内置动作无需远程执行（no-op 成功）；
		// 但显式脚本动作必须指定可信主机。
		if len(plan.Scripts) > 0 {
			return nil, ErrNoHost
		}
		plan.Actions = nil
		return plan, nil
	}
	return plan, nil
}

// Validate 校验计划可信性（主机白名单 + 脚本哈希白名单），在执行前同步拒绝。
func (c *Cleaner) Validate(plan *Plan) error {
	if plan == nil {
		return ErrInvalidPlan
	}
	if len(plan.Actions) == 0 && len(plan.Scripts) == 0 {
		// 无操作计划 = 无需清理（no-op 成功）。
		return nil
	}
	if len(plan.Hosts) == 0 {
		return ErrNoHost
	}
	for _, hostID := range plan.Hosts {
		if !c.allow.HostIDs[hostID] {
			return fmt.Errorf("%w: host_id=%d", ErrHostNotAllowlisted, hostID)
		}
	}
	for i := range plan.Scripts {
		script := &plan.Scripts[i]
		if script.HostID <= 0 || !c.allow.HostIDs[script.HostID] {
			return fmt.Errorf("%w: host_id=%d", ErrHostNotAllowlisted, script.HostID)
		}
		if !validScriptPath(script.Path) {
			return fmt.Errorf("%w: 脚本路径非法", ErrInvalidPlan)
		}
		hash, err := scriptHash(script)
		if err != nil {
			return err
		}
		if !c.allow.ScriptHashes[hash] {
			return fmt.Errorf("%w: path=%s hash=%s", ErrScriptNotAllowlisted, script.Path, hash)
		}
	}
	return nil
}

// Run 执行清理（校验失败即拒绝，不建立任何远程连接）。
func (c *Cleaner) Run(ctx context.Context, plan *Plan) (Result, error) {
	if err := c.Validate(plan); err != nil {
		return Result{}, err
	}
	if c.runner == nil {
		return Result{}, ErrNoRunner
	}
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	res := Result{Steps: make([]StepResult, 0, len(plan.Actions)*len(plan.Hosts)+len(plan.Scripts))}
	var errs []error

	for _, hostID := range plan.Hosts {
		for _, action := range plan.Actions {
			step := StepResult{Action: action, HostID: hostID, Path: cleanupHelperPath}
			out, err := c.runner.Run(ctx, hostID, cleanupHelperPath, []string{builtinActions[action]})
			step.Output, step.Err = out, err
			if err != nil {
				errs = append(errs, fmt.Errorf("动作 %s 在主机 %d 执行失败: %w", action, hostID, err))
			}
			res.Steps = append(res.Steps, step)
		}
	}

	for _, script := range plan.Scripts {
		step := StepResult{Action: ActionScript, HostID: script.HostID, Path: script.Path}
		out, err := c.runner.Run(ctx, script.HostID, script.Path, nil)
		step.Output, step.Err = out, err
		if err != nil {
			errs = append(errs, fmt.Errorf("脚本 %s 在主机 %d 执行失败: %w", script.Path, script.HostID, err))
		}
		res.Steps = append(res.Steps, step)
	}

	if len(errs) > 0 {
		return res, errors.Join(errs...)
	}
	return res, nil
}

// validScriptPath 校验绝对路径且不含路径穿越段。
func validScriptPath(path string) bool {
	if !scriptPathRE.MatchString(path) {
		return false
	}
	for _, seg := range strings.Split(path, "/") {
		if seg == ".." {
			return false
		}
	}
	return true
}

// scriptHash 计算脚本哈希：优先内容哈希，其次声明的 SHA256（均需命中白名单）。
func scriptHash(script *Script) (string, error) {
	if script.Content != "" {
		sum := sha256.Sum256([]byte(script.Content))
		return hex.EncodeToString(sum[:]), nil
	}
	if script.SHA256 == "" {
		return "", fmt.Errorf("%w: 脚本缺少内容与哈希，无法校验", ErrScriptNotAllowlisted)
	}
	return script.SHA256, nil
}

// dedupeHosts 主机 ID 去重（保持顺序）。
func dedupeHosts(ids []int64) []int64 {
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
