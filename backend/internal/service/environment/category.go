package environment

import (
	"context"
	"strings"
	"time"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
)

// CategoryInput 环境分类新建/修改输入（白名单字段）。
type CategoryInput struct {
	Name        string
	Description string
}

// CategoryView 环境分类视图。
type CategoryView struct {
	ID          int64
	Name        string
	Description string
	Version     int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ListCategories 环境分类列表（按 ID 稳定排序）。
func (s *Service) ListCategories(ctx context.Context) ([]CategoryView, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	categories, err := s.repo.ListCategories(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]CategoryView, 0, len(categories))
	for i := range categories {
		out = append(out, toCategoryView(&categories[i]))
	}
	return out, nil
}

// CreateCategory 新建环境分类（名称必填、长度受限、同名冲突 → 10302）。
func (s *Service) CreateCategory(ctx context.Context, in CategoryInput) (*CategoryView, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	name, description, err := validateCategoryInput(in)
	if err != nil {
		return nil, err
	}

	category := &domain.EnvironmentCategory{Name: name, Description: description}
	if err := s.repo.CreateCategory(ctx, category); err != nil {
		return nil, err
	}
	view := toCategoryView(category)
	return &view, nil
}

// UpdateCategory 修改环境分类（不存在 → 10301）。
func (s *Service) UpdateCategory(ctx context.Context, categoryID int64, in CategoryInput) (*CategoryView, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	if categoryID <= 0 {
		return nil, errcode.ErrParamFmt
	}
	if _, err := s.repo.GetCategory(ctx, categoryID); err != nil {
		return nil, err
	}

	name, description, err := validateCategoryInput(in)
	if err != nil {
		return nil, err
	}
	category := &domain.EnvironmentCategory{ID: categoryID, Name: name, Description: description}
	if err := s.repo.UpdateCategory(ctx, category); err != nil {
		return nil, err
	}

	updated, err := s.repo.GetCategory(ctx, categoryID)
	if err != nil {
		return nil, err
	}
	view := toCategoryView(updated)
	return &view, nil
}

// DeleteCategory 删除环境分类；被环境引用时禁止删除（21002，INV-6）。
func (s *Service) DeleteCategory(ctx context.Context, categoryID int64) error {
	if s == nil || s.repo == nil {
		return errcode.ErrDependency
	}
	if categoryID <= 0 {
		return errcode.ErrParamFmt
	}
	if _, err := s.repo.GetCategory(ctx, categoryID); err != nil {
		return err
	}
	refs, err := s.repo.CountEnvironmentsByCategory(ctx, categoryID)
	if err != nil {
		return err
	}
	if refs > 0 {
		return ErrCategoryRefed
	}
	return s.repo.SoftDeleteCategory(ctx, categoryID)
}

// validateCategoryInput 校验并归一化分类输入（名称必填、≤64 字符）。
func validateCategoryInput(in CategoryInput) (string, string, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return "", "", errcode.ErrParamMiss
	}
	if len([]rune(name)) > 64 {
		return "", "", errcode.ErrParamFmt
	}
	description := strings.TrimSpace(in.Description)
	if len([]rune(description)) > 500 {
		return "", "", errcode.ErrParamFmt
	}
	return name, description, nil
}

// toCategoryView 领域实体 → 视图。
func toCategoryView(c *domain.EnvironmentCategory) CategoryView {
	return CategoryView{
		ID:          c.ID,
		Name:        c.Name,
		Description: c.Description,
		Version:     c.Version,
		CreatedAt:   c.CreatedAt,
		UpdatedAt:   c.UpdatedAt,
	}
}
