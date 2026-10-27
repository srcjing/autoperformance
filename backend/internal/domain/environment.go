// Package domain 领域模型（实体 + 枚举/状态机）。
//
// 本文件定义环境管理实体（EnvironmentCategory/Tag/Environment）、状态机与查询条件，
// 对齐 PRD §4.1/§4.2/§4.3、§7.1 与 api-contract §2.2。
package domain

import "time"

// 环境状态枚举（PRD §7.1；api-contract §2.2 status）。
const (
	// EnvStatusPending 待测试（可被新任务选择）。
	EnvStatusPending = "pending"
	// EnvStatusTesting 测试中（被任务占用，禁止删除/停用）。
	EnvStatusTesting = "testing"
	// EnvStatusDisabled 已下线（停用，不出现在新任务可选列表）。
	EnvStatusDisabled = "disabled"
)

// 环境类型枚举（PRD §4.3）。
const (
	EnvTypeSingle  = "single"
	EnvTypeCluster = "cluster"
)

// IsValidEnvStatus 判断环境状态取值是否合法。
func IsValidEnvStatus(status string) bool {
	switch status {
	case EnvStatusPending, EnvStatusTesting, EnvStatusDisabled:
		return true
	default:
		return false
	}
}

// IsValidEnvType 判断环境类型取值是否合法。
func IsValidEnvType(envType string) bool {
	return envType == EnvTypeSingle || envType == EnvTypeCluster
}

// CanTransitEnvStatus 判断手动状态流转是否合法：
//   - 手动流转仅 pending ↔ disabled（停用/启用）；
//   - testing 的进入/退出由任务编排驱动（Phase 4），手动不得直接改动；
//   - 同状态重复设置视为非法（显式暴露状态冲突，避免前端误以为已变更）。
func CanTransitEnvStatus(from, to string) bool {
	if !IsValidEnvStatus(from) || !IsValidEnvStatus(to) || from == to {
		return false
	}
	if from == EnvStatusTesting || to == EnvStatusTesting {
		return false
	}
	return true
}

// CanMarkEnvTesting 判断环境是否可被任务占用（pending → testing）。
func CanMarkEnvTesting(from string) bool { return from == EnvStatusPending }

// CanReleaseEnvTesting 判断环境是否可释放占用（testing → pending）。
func CanReleaseEnvTesting(from string) bool { return from == EnvStatusTesting }

// EnvironmentCategory 环境分类（PRD §4.1）。
type EnvironmentCategory struct {
	ID          int64
	Name        string
	Description string
	Version     int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Tag 标签（PRD §4.2）；EnvironmentCount 为关联环境数（列表查询时填充）。
type Tag struct {
	ID               int64
	Name             string
	Color            string
	Description      string
	EnvironmentCount int64
	Version          int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Environment 环境（PRD §4.3）。
//
// TagIDs 为环境↔标签多对多关联（查询时填充）；SshHostID/CertID 为可空的
// 引用（0 表示未绑定），对应 DB 的 NULL。
type Environment struct {
	ID          int64
	Name        string
	Code        string
	CategoryID  int64
	Type        string
	Hosts       []string
	OS          string
	SshHostID   int64
	CertID      int64
	Description string
	Status      string
	Version     int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
	TagIDs      []int64
}

// IsTesting 环境是否处于测试中（被任务占用）。
func (e *Environment) IsTesting() bool { return e != nil && e.Status == EnvStatusTesting }

// EnvironmentQuery 环境列表查询条件（仓储层按此过滤；TagIDs 取交集）。
type EnvironmentQuery struct {
	Keyword    string
	CategoryID int64
	TagIDs     []int64
	Status     string
	Page       int
	PageSize   int
}
