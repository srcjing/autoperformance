package testing

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
)

// CreateTemplateInput 模板新建/修改输入（白名单字段；version 为乐观锁版本号）。
type CreateTemplateInput struct {
	Name         string
	EnvIDs       []int64
	InterfaceIDs []int64
	Flow         json.RawMessage
	ReportConfig json.RawMessage
	CleanConfig  json.RawMessage
	Description  string
	Version      int64
}

// TemplateView 模板视图。
type TemplateView struct {
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

// TemplatePage 模板分页结果。
type TemplatePage struct {
	List     []TemplateView
	Total    int64
	Page     int
	PageSize int
}

// ListTemplatesQuery 模板列表查询条件。
type ListTemplatesQuery struct {
	Keyword  string
	Page     int
	PageSize int
}

// ListTemplates 模板列表（关键字 + 分页）。
func (s *Service) ListTemplates(ctx context.Context, q ListTemplatesQuery) (*TemplatePage, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	page, pageSize := normalizePaging(q.Page, q.PageSize)
	items, total, err := s.repo.ListTemplates(ctx, domain.TemplateQuery{
		Keyword:  strings.TrimSpace(q.Keyword),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		return nil, err
	}
	list := make([]TemplateView, 0, len(items))
	for i := range items {
		list = append(list, toTemplateView(&items[i]))
	}
	return &TemplatePage{List: list, Total: total, Page: page, PageSize: pageSize}, nil
}

// GetTemplate 模板详情（不存在 → 23005）。
func (s *Service) GetTemplate(ctx context.Context, id int64) (*TemplateView, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	if id <= 0 {
		return nil, errcode.ErrParamFmt
	}
	tpl, err := s.repo.GetTemplate(ctx, id)
	if err != nil {
		if errors.Is(err, errcode.ErrNotFound) {
			return nil, ErrTemplateNotFound
		}
		return nil, err
	}
	view := toTemplateView(tpl)
	return &view, nil
}

// CreateTemplate 新建模板（流程/报告/清理配置均为必填并做白名单校验）。
func (s *Service) CreateTemplate(ctx context.Context, in CreateTemplateInput) (*TemplateView, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	tpl, err := s.validateTemplateInput(in)
	if err != nil {
		return nil, err
	}
	if err := s.repo.CreateTemplate(ctx, tpl); err != nil {
		return nil, err
	}
	return s.GetTemplate(ctx, tpl.ID)
}

// UpdateTemplate 全量修改模板（version 乐观锁：缺失 → 10002，冲突 → 10402）。
func (s *Service) UpdateTemplate(ctx context.Context, id int64, in CreateTemplateInput) (*TemplateView, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	if id <= 0 {
		return nil, errcode.ErrParamFmt
	}
	if in.Version <= 0 {
		return nil, errcode.ErrParamMiss
	}
	if _, err := s.GetTemplate(ctx, id); err != nil {
		return nil, err
	}
	tpl, err := s.validateTemplateInput(in)
	if err != nil {
		return nil, err
	}
	tpl.ID = id
	if err := s.repo.UpdateTemplate(ctx, tpl, in.Version); err != nil {
		return nil, err
	}
	return s.GetTemplate(ctx, id)
}

// DeleteTemplate 删除模板（逻辑删除；不存在 → 23005）。
func (s *Service) DeleteTemplate(ctx context.Context, id int64) error {
	if s == nil || s.repo == nil {
		return errcode.ErrDependency
	}
	if id <= 0 {
		return errcode.ErrParamFmt
	}
	if _, err := s.GetTemplate(ctx, id); err != nil {
		return err
	}
	return s.repo.SoftDeleteTemplate(ctx, id)
}

// validateTemplateInput 校验模板输入并归一化配置（INV-9）。
func (s *Service) validateTemplateInput(in CreateTemplateInput) (*domain.TestTemplate, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, errcode.ErrParamMiss
	}
	if len([]rune(name)) > 128 {
		return nil, errcode.ErrParamFmt
	}
	if len(in.Flow) == 0 || len(in.ReportConfig) == 0 || len(in.CleanConfig) == 0 {
		return nil, errcode.ErrParamMiss
	}
	if !configSizeOK(in.Flow, in.ReportConfig, in.CleanConfig) {
		return nil, errcode.ErrParamFmt
	}
	if len(in.InterfaceIDs) > domain.MaxInterfaceRange {
		return nil, errcode.ErrParamFmt
	}
	description := strings.TrimSpace(in.Description)
	if len([]rune(description)) > 500 {
		return nil, errcode.ErrParamFmt
	}

	flowCfg, err := domain.ParseFlow(in.Flow)
	if err != nil {
		return nil, errcode.ErrParamFmt
	}
	if _, err := flowCfg.Params(s.maxVUs); err != nil {
		return nil, mapFlowError(err)
	}
	normalizedReport, _, err := domain.ParseReportConfig(in.ReportConfig)
	if err != nil {
		return nil, mapConfigError(err)
	}
	normalizedClean, _, err := domain.ParseCleanConfig(in.CleanConfig)
	if err != nil {
		return nil, mapConfigError(err)
	}

	return &domain.TestTemplate{
		Name:         name,
		EnvIDs:       dedupeIDs(in.EnvIDs),
		InterfaceIDs: dedupeIDs(in.InterfaceIDs),
		Flow:         in.Flow,
		ReportConfig: normalizedReport,
		CleanConfig:  normalizedClean,
		Description:  description,
	}, nil
}

// toTemplateView 领域模板 → 视图。
func toTemplateView(t *domain.TestTemplate) TemplateView {
	return TemplateView{
		ID:           t.ID,
		Name:         t.Name,
		EnvIDs:       append([]int64{}, t.EnvIDs...),
		InterfaceIDs: append([]int64{}, t.InterfaceIDs...),
		Flow:         t.Flow,
		ReportConfig: t.ReportConfig,
		CleanConfig:  t.CleanConfig,
		Description:  t.Description,
		Version:      t.Version,
		CreatedAt:    t.CreatedAt,
		UpdatedAt:    t.UpdatedAt,
	}
}
