// Package environment 环境管理业务服务（分类/标签/环境 CRUD 与标签绑定）。
//
// 对齐 PRD §3.1/§4.1~§4.3/§7.1、api-contract §2.2 与 INV-6（引用保护）：
//   - 分类/标签被环境引用时禁止删除（21002/21003）；
//   - 环境 testing 时禁止删除/停用（21004）；
//   - 全量修改携带 version 乐观锁，冲突返回 10402（DES-T-001）；
//   - 全部删除均为逻辑删除，历史关联不丢失。
package environment

import (
	"context"
	"errors"
	"strings"
	"time"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
)

// DefaultPageSize 列表默认页大小；MaxPageSize 分页上限（api-contract §1.3）。
const (
	DefaultPageSize = 20
	MaxPageSize     = 200
)

// maxHosts 单环境主机地址数量上限（保护请求体与展示）。
const maxHosts = 100

// Repo 环境管理仓储能力（由 repository.PGRepository 实现）。
type Repo interface {
	// ===== 分类 =====
	ListCategories(ctx context.Context) ([]domain.EnvironmentCategory, error)
	GetCategory(ctx context.Context, categoryID int64) (*domain.EnvironmentCategory, error)
	CreateCategory(ctx context.Context, category *domain.EnvironmentCategory) error
	UpdateCategory(ctx context.Context, category *domain.EnvironmentCategory) error
	SoftDeleteCategory(ctx context.Context, categoryID int64) error
	CountEnvironmentsByCategory(ctx context.Context, categoryID int64) (int64, error)

	// ===== 标签 =====
	ListTags(ctx context.Context) ([]domain.Tag, error)
	GetTag(ctx context.Context, tagID int64) (*domain.Tag, error)
	CreateTag(ctx context.Context, tag *domain.Tag) error
	UpdateTag(ctx context.Context, tag *domain.Tag) error
	SoftDeleteTag(ctx context.Context, tagID int64) error
	CountEnvironmentsByTag(ctx context.Context, tagID int64) (int64, error)
	CountTagsByIDs(ctx context.Context, tagIDs []int64) (int64, error)

	// ===== 环境 =====
	ListEnvironments(ctx context.Context, q domain.EnvironmentQuery) ([]domain.Environment, int64, error)
	EnvironmentsByTag(ctx context.Context, tagID int64, page, pageSize int) ([]domain.Environment, int64, error)
	GetEnvironment(ctx context.Context, envID int64) (*domain.Environment, error)
	CreateEnvironment(ctx context.Context, env *domain.Environment) error
	UpdateEnvironment(ctx context.Context, env *domain.Environment, version int64) error
	UpdateEnvironmentStatus(ctx context.Context, envID int64, status string) error
	SoftDeleteEnvironment(ctx context.Context, envID int64) error
	ReplaceEnvironmentTags(ctx context.Context, envID int64, tagIDs []int64) error
}

// Service 环境管理业务服务。
type Service struct {
	repo Repo
}

// New 构造环境管理服务。
func New(repo Repo) *Service { return &Service{repo: repo} }

// EnvironmentInput 环境新建/修改输入（白名单字段；status 只能经状态接口变更）。
type EnvironmentInput struct {
	Name        string
	Code        string
	CategoryID  int64
	TagIDs      []int64
	Type        string
	Hosts       []string
	OS          string
	SshHostID   int64
	CertID      int64
	Description string
	Version     int64
}

// EnvironmentView 环境视图（含分类名称与标签摘要）。
type EnvironmentView struct {
	ID           int64
	Name         string
	Code         string
	CategoryID   int64
	CategoryName string
	Tags         []TagBrief
	Type         string
	Hosts        []string
	OS           string
	SshHostID    int64
	CertID       int64
	Description  string
	Status       string
	Version      int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// EnvironmentPage 环境分页结果（对齐 api-contract §1.3）。
type EnvironmentPage struct {
	List     []EnvironmentView
	Total    int64
	Page     int
	PageSize int
}

// ListEnvironmentsQuery 环境列表查询（标签 ID 取交集）。
type ListEnvironmentsQuery struct {
	Keyword    string
	CategoryID int64
	TagIDs     []int64
	Status     string
	Page       int
	PageSize   int
}

// normalizePaging 归一化分页参数（默认 20，上限 200）。
func normalizePaging(page, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = DefaultPageSize
	}
	if pageSize > MaxPageSize {
		pageSize = MaxPageSize
	}
	return page, pageSize
}

// dedupeIDs 去重并过滤非正 ID（保持原顺序）。
func dedupeIDs(ids []int64) []int64 {
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

// ListEnvironments 环境列表（关键字/分类/标签交集/状态组合筛选 + 分页）。
func (s *Service) ListEnvironments(ctx context.Context, q ListEnvironmentsQuery) (*EnvironmentPage, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	status := strings.TrimSpace(q.Status)
	if status != "" && !domain.IsValidEnvStatus(status) {
		return nil, errcode.ErrParamFmt
	}
	if q.CategoryID < 0 {
		return nil, errcode.ErrParamFmt
	}

	page, pageSize := normalizePaging(q.Page, q.PageSize)
	envs, total, err := s.repo.ListEnvironments(ctx, domain.EnvironmentQuery{
		Keyword:    strings.TrimSpace(q.Keyword),
		CategoryID: q.CategoryID,
		TagIDs:     dedupeIDs(q.TagIDs),
		Status:     status,
		Page:       page,
		PageSize:   pageSize,
	})
	if err != nil {
		return nil, err
	}

	list, err := s.buildEnvironmentViews(ctx, envs)
	if err != nil {
		return nil, err
	}
	return &EnvironmentPage{List: list, Total: total, Page: page, PageSize: pageSize}, nil
}

// GetEnvironment 环境详情（不存在 → 10301）。
func (s *Service) GetEnvironment(ctx context.Context, envID int64) (*EnvironmentView, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	if envID <= 0 {
		return nil, errcode.ErrParamFmt
	}
	env, err := s.repo.GetEnvironment(ctx, envID)
	if err != nil {
		return nil, err
	}
	views, err := s.buildEnvironmentViews(ctx, []domain.Environment{*env})
	if err != nil {
		return nil, err
	}
	return &views[0], nil
}

// CreateEnvironment 新建环境（分类必填、编码唯一、标签须预定义，初始 pending）。
func (s *Service) CreateEnvironment(ctx context.Context, in EnvironmentInput) (*EnvironmentView, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}

	env, tagIDs, err := s.validateEnvironmentInput(ctx, in)
	if err != nil {
		return nil, err
	}
	env.Status = domain.EnvStatusPending // 状态只能由状态机变更，不接受请求体指定

	if err := s.repo.CreateEnvironment(ctx, env); err != nil {
		if errors.Is(err, errcode.ErrConflict) {
			return nil, ErrCodeExists
		}
		return nil, err
	}
	if err := s.repo.ReplaceEnvironmentTags(ctx, env.ID, tagIDs); err != nil {
		return nil, err
	}
	return s.GetEnvironment(ctx, env.ID)
}

// UpdateEnvironment 全量修改环境（version 乐观锁；状态保持当前值）。
func (s *Service) UpdateEnvironment(ctx context.Context, envID int64, in EnvironmentInput) (*EnvironmentView, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	if envID <= 0 {
		return nil, errcode.ErrParamFmt
	}
	if in.Version <= 0 {
		// 全量修改必须携带 version，避免并发覆盖（api-contract §2.2，DES-T-001）。
		return nil, errcode.ErrParamMiss
	}

	current, err := s.repo.GetEnvironment(ctx, envID)
	if err != nil {
		return nil, err
	}
	env, tagIDs, err := s.validateEnvironmentInput(ctx, in)
	if err != nil {
		return nil, err
	}
	env.ID = envID
	env.Status = current.Status // 状态仅能经 PATCH /status 变更

	if err := s.repo.UpdateEnvironment(ctx, env, in.Version); err != nil {
		if errors.Is(err, errcode.ErrConflict) {
			return nil, ErrCodeExists
		}
		return nil, err
	}
	// 请求体未携带 tag_ids（nil）表示不改标签；显式 [] 表示清空。
	if in.TagIDs != nil {
		if err := s.repo.ReplaceEnvironmentTags(ctx, envID, tagIDs); err != nil {
			return nil, err
		}
	}
	return s.GetEnvironment(ctx, envID)
}

// DeleteEnvironment 逻辑删除环境；testing 时禁止删除（21004）。
func (s *Service) DeleteEnvironment(ctx context.Context, envID int64) error {
	if s == nil || s.repo == nil {
		return errcode.ErrDependency
	}
	if envID <= 0 {
		return errcode.ErrParamFmt
	}
	env, err := s.repo.GetEnvironment(ctx, envID)
	if err != nil {
		return err
	}
	if env.IsTesting() {
		return ErrEnvOccupied
	}
	if err := s.repo.SoftDeleteEnvironment(ctx, envID); err != nil {
		// DB 侧 status<>'testing' 兑底命中（并发占用竞态）统一转为业务码 21004。
		if errors.Is(err, errcode.ErrEnvBusy) {
			return ErrEnvOccupied
		}
		return err
	}
	return nil
}

// UpdateStatus 停用/启用环境（手动状态机 pending ↔ disabled；testing → 21004/10401）。
func (s *Service) UpdateStatus(ctx context.Context, envID int64, status string) error {
	if s == nil || s.repo == nil {
		return errcode.ErrDependency
	}
	if envID <= 0 {
		return errcode.ErrParamFmt
	}
	if status != domain.EnvStatusPending && status != domain.EnvStatusDisabled {
		// testing 由任务编排驱动（Phase 4），PATCH 接口不接受。
		return errcode.ErrParamFmt
	}

	env, err := s.repo.GetEnvironment(ctx, envID)
	if err != nil {
		return err
	}
	if env.IsTesting() {
		if status == domain.EnvStatusDisabled {
			return ErrEnvOccupied
		}
		return errcode.ErrState
	}
	if !domain.CanTransitEnvStatus(env.Status, status) {
		return errcode.ErrState
	}
	if err := s.repo.UpdateEnvironmentStatus(ctx, envID, status); err != nil {
		// DB 侧 status<>'testing' 兑底命中（并发占用竞态）统一转为业务码 21004。
		if errors.Is(err, errcode.ErrEnvBusy) {
			return ErrEnvOccupied
		}
		return err
	}
	return nil
}

// BindTags 全量替换环境标签（标签必须已预定义；显式空列表表示清空）。
func (s *Service) BindTags(ctx context.Context, envID int64, tagIDs []int64) error {
	if s == nil || s.repo == nil {
		return errcode.ErrDependency
	}
	if envID <= 0 {
		return errcode.ErrParamFmt
	}
	if _, err := s.repo.GetEnvironment(ctx, envID); err != nil {
		return err
	}

	ids := dedupeIDs(tagIDs)
	if err := s.ensureTagsExist(ctx, ids); err != nil {
		return err
	}
	return s.repo.ReplaceEnvironmentTags(ctx, envID, ids)
}

// validateEnvironmentInput 校验并归一化环境输入，返回领域实体与去重后的标签 ID。
func (s *Service) validateEnvironmentInput(ctx context.Context, in EnvironmentInput) (*domain.Environment, []int64, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, nil, errcode.ErrParamMiss
	}
	if len([]rune(name)) > 128 {
		return nil, nil, errcode.ErrParamFmt
	}

	code := strings.TrimSpace(in.Code)
	if code == "" {
		return nil, nil, errcode.ErrParamMiss
	}
	if len(code) > 64 {
		return nil, nil, errcode.ErrParamFmt
	}

	if in.CategoryID <= 0 {
		return nil, nil, errcode.ErrParamMiss // 分类必填（api-contract §2.2）
	}
	if _, err := s.repo.GetCategory(ctx, in.CategoryID); err != nil {
		return nil, nil, err
	}

	envType := strings.TrimSpace(in.Type)
	if envType == "" {
		envType = domain.EnvTypeSingle
	}
	if !domain.IsValidEnvType(envType) {
		return nil, nil, errcode.ErrParamFmt
	}

	hosts, err := normalizeHosts(in.Hosts)
	if err != nil {
		return nil, nil, err
	}

	tagIDs := dedupeIDs(in.TagIDs)
	if err := s.ensureTagsExist(ctx, tagIDs); err != nil {
		return nil, nil, err
	}

	description := strings.TrimSpace(in.Description)
	if len([]rune(description)) > 500 {
		return nil, nil, errcode.ErrParamFmt
	}
	if in.SshHostID < 0 || in.CertID < 0 {
		return nil, nil, errcode.ErrParamFmt
	}

	env := &domain.Environment{
		Name:        name,
		Code:        code,
		CategoryID:  in.CategoryID,
		Type:        envType,
		Hosts:       hosts,
		OS:          strings.TrimSpace(in.OS),
		SshHostID:   in.SshHostID,
		CertID:      in.CertID,
		Description: description,
	}
	return env, tagIDs, nil
}

// normalizeHosts 归一化主机地址列表：去空白、去空项，要求至少一个且数量受限。
func normalizeHosts(raw []string) ([]string, error) {
	hosts := make([]string, 0, len(raw))
	for _, h := range raw {
		v := strings.TrimSpace(h)
		if v == "" {
			continue
		}
		if len(v) > 255 {
			return nil, errcode.ErrParamFmt
		}
		hosts = append(hosts, v)
	}
	if len(hosts) == 0 {
		return nil, errcode.ErrParamMiss // 主机地址列表必填（PRD §4.3）
	}
	if len(hosts) > maxHosts {
		return nil, errcode.ErrParamFmt
	}
	return hosts, nil
}

// ensureTagsExist 校验标签均已预定义（否则 10301，禁止自由文本标签）。
func (s *Service) ensureTagsExist(ctx context.Context, tagIDs []int64) error {
	if len(tagIDs) == 0 {
		return nil
	}
	count, err := s.repo.CountTagsByIDs(ctx, tagIDs)
	if err != nil {
		return err
	}
	if count != int64(len(tagIDs)) {
		return errcode.ErrNotFound
	}
	return nil
}

// buildEnvironmentViews 批量装配环境视图（一次取分类/标签，避免 N+1）。
func (s *Service) buildEnvironmentViews(ctx context.Context, envs []domain.Environment) ([]EnvironmentView, error) {
	out := make([]EnvironmentView, 0, len(envs))
	if len(envs) == 0 {
		return out, nil
	}

	categories, err := s.repo.ListCategories(ctx)
	if err != nil {
		return nil, err
	}
	categoryNames := make(map[int64]string, len(categories))
	for i := range categories {
		categoryNames[categories[i].ID] = categories[i].Name
	}

	tags, err := s.repo.ListTags(ctx)
	if err != nil {
		return nil, err
	}
	tagByID := make(map[int64]domain.Tag, len(tags))
	for i := range tags {
		tagByID[tags[i].ID] = tags[i]
	}

	for i := range envs {
		env := &envs[i]
		briefs := make([]TagBrief, 0, len(env.TagIDs))
		for _, id := range env.TagIDs {
			if tag, ok := tagByID[id]; ok {
				briefs = append(briefs, TagBrief{ID: tag.ID, Name: tag.Name, Color: tag.Color})
			}
		}
		out = append(out, EnvironmentView{
			ID:           env.ID,
			Name:         env.Name,
			Code:         env.Code,
			CategoryID:   env.CategoryID,
			CategoryName: categoryNames[env.CategoryID],
			Tags:         briefs,
			Type:         env.Type,
			Hosts:        append([]string(nil), env.Hosts...),
			OS:           env.OS,
			SshHostID:    env.SshHostID,
			CertID:       env.CertID,
			Description:  env.Description,
			Status:       env.Status,
			Version:      env.Version,
			CreatedAt:    env.CreatedAt,
			UpdatedAt:    env.UpdatedAt,
		})
	}
	return out, nil
}
