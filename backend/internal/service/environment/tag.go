package environment

import (
	"context"
	"regexp"
	"strings"
	"time"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
)

// colorPattern 标签颜色格式（前端取色器输出 #RRGGBB；非必填）。
var colorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// TagInput 标签新建/修改输入（白名单字段）。
type TagInput struct {
	Name        string
	Color       string
	Description string
}

// TagView 标签视图（含关联环境数）。
type TagView struct {
	ID               int64
	Name             string
	Color            string
	Description      string
	EnvironmentCount int64
	Version          int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// TagBrief 环境视图内嵌的标签摘要。
type TagBrief struct {
	ID    int64
	Name  string
	Color string
}

// ListTagsQuery 标签列表查询（关键字匹配名称/说明）。
type ListTagsQuery struct {
	Keyword string
}

// ListTags 标签列表（含关联环境数，INV-6 删前提示依据）。
func (s *Service) ListTags(ctx context.Context, q ListTagsQuery) ([]TagView, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	tags, err := s.repo.ListTags(ctx)
	if err != nil {
		return nil, err
	}
	keyword := strings.TrimSpace(q.Keyword)
	out := make([]TagView, 0, len(tags))
	for i := range tags {
		if keyword != "" && !strings.Contains(tags[i].Name, keyword) && !strings.Contains(tags[i].Description, keyword) {
			continue
		}
		out = append(out, toTagView(&tags[i]))
	}
	return out, nil
}

// GetTag 标签详情（不存在 → 10301）。
func (s *Service) GetTag(ctx context.Context, tagID int64) (*TagView, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	if tagID <= 0 {
		return nil, errcode.ErrParamFmt
	}
	tag, err := s.repo.GetTag(ctx, tagID)
	if err != nil {
		return nil, err
	}
	view := toTagView(tag)
	return &view, nil
}

// CreateTag 新建标签（名称必填、≤64、同名冲突 → 10302）。
func (s *Service) CreateTag(ctx context.Context, in TagInput) (*TagView, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	name, color, description, err := validateTagInput(in)
	if err != nil {
		return nil, err
	}

	tag := &domain.Tag{Name: name, Color: color, Description: description}
	if err := s.repo.CreateTag(ctx, tag); err != nil {
		return nil, err
	}
	view := toTagView(tag)
	return &view, nil
}

// UpdateTag 修改标签（不存在 → 10301）。
func (s *Service) UpdateTag(ctx context.Context, tagID int64, in TagInput) (*TagView, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	if tagID <= 0 {
		return nil, errcode.ErrParamFmt
	}
	if _, err := s.repo.GetTag(ctx, tagID); err != nil {
		return nil, err
	}

	name, color, description, err := validateTagInput(in)
	if err != nil {
		return nil, err
	}
	tag := &domain.Tag{ID: tagID, Name: name, Color: color, Description: description}
	if err := s.repo.UpdateTag(ctx, tag); err != nil {
		return nil, err
	}
	return s.GetTag(ctx, tagID)
}

// DeleteTag 删除标签；被环境引用时禁止删除（21003，INV-6）。
func (s *Service) DeleteTag(ctx context.Context, tagID int64) error {
	if s == nil || s.repo == nil {
		return errcode.ErrDependency
	}
	if tagID <= 0 {
		return errcode.ErrParamFmt
	}
	if _, err := s.repo.GetTag(ctx, tagID); err != nil {
		return err
	}
	refs, err := s.repo.CountEnvironmentsByTag(ctx, tagID)
	if err != nil {
		return err
	}
	if refs > 0 {
		return ErrTagRefed
	}
	return s.repo.SoftDeleteTag(ctx, tagID)
}

// TagEnvironments 标签反查关联环境（分页；标签不存在 → 10301）。
func (s *Service) TagEnvironments(ctx context.Context, tagID int64, page, pageSize int) (*EnvironmentPage, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	if tagID <= 0 {
		return nil, errcode.ErrParamFmt
	}
	if _, err := s.repo.GetTag(ctx, tagID); err != nil {
		return nil, err
	}
	page, pageSize = normalizePaging(page, pageSize)
	envs, total, err := s.repo.EnvironmentsByTag(ctx, tagID, page, pageSize)
	if err != nil {
		return nil, err
	}
	views, err := s.buildEnvironmentViews(ctx, envs)
	if err != nil {
		return nil, err
	}
	return &EnvironmentPage{List: views, Total: total, Page: page, PageSize: pageSize}, nil
}

// validateTagInput 校验并归一化标签输入（名称必填、≤64；颜色可选但须为 #RRGGBB）。
func validateTagInput(in TagInput) (string, string, string, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return "", "", "", errcode.ErrParamMiss
	}
	if len([]rune(name)) > 64 {
		return "", "", "", errcode.ErrParamFmt
	}
	color := strings.TrimSpace(in.Color)
	if color != "" && !colorPattern.MatchString(color) {
		return "", "", "", errcode.ErrParamFmt
	}
	description := strings.TrimSpace(in.Description)
	if len([]rune(description)) > 500 {
		return "", "", "", errcode.ErrParamFmt
	}
	return name, color, description, nil
}

// toTagView 领域实体 → 视图。
func toTagView(t *domain.Tag) TagView {
	return TagView{
		ID:               t.ID,
		Name:             t.Name,
		Color:            t.Color,
		Description:      t.Description,
		EnvironmentCount: t.EnvironmentCount,
		Version:          t.Version,
		CreatedAt:        t.CreatedAt,
		UpdatedAt:        t.UpdatedAt,
	}
}
