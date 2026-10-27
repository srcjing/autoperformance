package environment

import (
	"context"
	"sort"
	"strings"
	"time"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
)

// fakeRepo 内存环境仓储（无 DB 依赖）：实现 Repo 全量方法，
// 供 service 层单测验证校验逻辑/引用保护/乐观锁/标签绑定。
type fakeRepo struct {
	categories map[int64]*domain.EnvironmentCategory
	tags       map[int64]*domain.Tag
	envs       map[int64]*domain.Environment
	envTags    map[int64][]int64 // env_id → tag_ids

	nextCategory int64
	nextTag      int64
	nextEnv      int64

	deletedCategories []int64
	deletedTags       []int64
	deletedEnvs       []int64
	tagReplaceCalls   int
	lastTagIDs        []int64
	updateEnvCalls    int
	statusCalls       map[int64]string
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		categories:   map[int64]*domain.EnvironmentCategory{},
		tags:         map[int64]*domain.Tag{},
		envs:         map[int64]*domain.Environment{},
		envTags:      map[int64][]int64{},
		nextCategory: 100,
		nextTag:      200,
		nextEnv:      300,
		statusCalls:  map[int64]string{},
	}
}

// ===================== 分类 =====================

func (f *fakeRepo) ListCategories(_ context.Context) ([]domain.EnvironmentCategory, error) {
	out := make([]domain.EnvironmentCategory, 0, len(f.categories))
	for _, c := range f.categories {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (f *fakeRepo) GetCategory(_ context.Context, categoryID int64) (*domain.EnvironmentCategory, error) {
	c, ok := f.categories[categoryID]
	if !ok {
		return nil, errcode.ErrNotFound
	}
	cp := *c
	return &cp, nil
}

func (f *fakeRepo) CreateCategory(_ context.Context, category *domain.EnvironmentCategory) error {
	for _, c := range f.categories {
		if c.Name == category.Name {
			return errcode.ErrConflict
		}
	}
	f.nextCategory++
	category.ID = f.nextCategory
	category.Version = 1
	category.CreatedAt = time.Now()
	category.UpdatedAt = category.CreatedAt
	cp := *category
	f.categories[category.ID] = &cp
	return nil
}

func (f *fakeRepo) UpdateCategory(_ context.Context, category *domain.EnvironmentCategory) error {
	c, ok := f.categories[category.ID]
	if !ok {
		return errcode.ErrNotFound
	}
	for id, other := range f.categories {
		if id != category.ID && other.Name == category.Name {
			return errcode.ErrConflict
		}
	}
	c.Name, c.Description = category.Name, category.Description
	c.Version++
	c.UpdatedAt = time.Now()
	category.Version = c.Version
	return nil
}

func (f *fakeRepo) SoftDeleteCategory(_ context.Context, categoryID int64) error {
	if _, ok := f.categories[categoryID]; !ok {
		return errcode.ErrNotFound
	}
	f.deletedCategories = append(f.deletedCategories, categoryID)
	delete(f.categories, categoryID)
	return nil
}

func (f *fakeRepo) CountEnvironmentsByCategory(_ context.Context, categoryID int64) (int64, error) {
	var n int64
	for _, e := range f.envs {
		if e.CategoryID == categoryID {
			n++
		}
	}
	return n, nil
}

// ===================== 标签 =====================

func (f *fakeRepo) ListTags(_ context.Context) ([]domain.Tag, error) {
	out := make([]domain.Tag, 0, len(f.tags))
	for _, t := range f.tags {
		cp := *t
		cp.EnvironmentCount = f.environmentCount(t.ID)
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (f *fakeRepo) GetTag(_ context.Context, tagID int64) (*domain.Tag, error) {
	t, ok := f.tags[tagID]
	if !ok {
		return nil, errcode.ErrNotFound
	}
	cp := *t
	cp.EnvironmentCount = f.environmentCount(tagID)
	return &cp, nil
}

func (f *fakeRepo) CreateTag(_ context.Context, tag *domain.Tag) error {
	for _, t := range f.tags {
		if t.Name == tag.Name {
			return errcode.ErrConflict
		}
	}
	f.nextTag++
	tag.ID = f.nextTag
	tag.Version = 1
	tag.CreatedAt = time.Now()
	tag.UpdatedAt = tag.CreatedAt
	cp := *tag
	f.tags[tag.ID] = &cp
	return nil
}

func (f *fakeRepo) UpdateTag(_ context.Context, tag *domain.Tag) error {
	t, ok := f.tags[tag.ID]
	if !ok {
		return errcode.ErrNotFound
	}
	for id, other := range f.tags {
		if id != tag.ID && other.Name == tag.Name {
			return errcode.ErrConflict
		}
	}
	t.Name, t.Color, t.Description = tag.Name, tag.Color, tag.Description
	t.Version++
	t.UpdatedAt = time.Now()
	tag.Version = t.Version
	return nil
}

func (f *fakeRepo) SoftDeleteTag(_ context.Context, tagID int64) error {
	if _, ok := f.tags[tagID]; !ok {
		return errcode.ErrNotFound
	}
	f.deletedTags = append(f.deletedTags, tagID)
	delete(f.tags, tagID)
	return nil
}

func (f *fakeRepo) CountEnvironmentsByTag(_ context.Context, tagID int64) (int64, error) {
	return f.environmentCount(tagID), nil
}

func (f *fakeRepo) CountTagsByIDs(_ context.Context, tagIDs []int64) (int64, error) {
	var n int64
	for _, id := range tagIDs {
		if _, ok := f.tags[id]; ok {
			n++
		}
	}
	return n, nil
}

// environmentCount 统计引用标签的（未删除）环境数。
func (f *fakeRepo) environmentCount(tagID int64) int64 {
	var n int64
	for envID, ids := range f.envTags {
		if _, ok := f.envs[envID]; !ok {
			continue
		}
		for _, id := range ids {
			if id == tagID {
				n++
				break
			}
		}
	}
	return n
}

// ===================== 环境 =====================

func (f *fakeRepo) ListEnvironments(_ context.Context, q domain.EnvironmentQuery) ([]domain.Environment, int64, error) {
	matched := make([]domain.Environment, 0, len(f.envs))
	for _, e := range f.envs {
		if q.Keyword != "" && !strings.Contains(e.Name, q.Keyword) && !strings.Contains(e.Code, q.Keyword) {
			continue
		}
		if q.CategoryID > 0 && e.CategoryID != q.CategoryID {
			continue
		}
		if q.Status != "" && e.Status != q.Status {
			continue
		}
		if len(q.TagIDs) > 0 && !containsAll(f.envTags[e.ID], q.TagIDs) {
			continue
		}
		matched = append(matched, *f.withTags(e))
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].ID < matched[j].ID })
	return paginate(matched, q.Page, q.PageSize), int64(len(matched)), nil
}

func (f *fakeRepo) EnvironmentsByTag(_ context.Context, tagID int64, page, pageSize int) ([]domain.Environment, int64, error) {
	matched := make([]domain.Environment, 0)
	for _, e := range f.envs {
		for _, id := range f.envTags[e.ID] {
			if id == tagID {
				matched = append(matched, *f.withTags(e))
				break
			}
		}
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].ID < matched[j].ID })
	return paginate(matched, page, pageSize), int64(len(matched)), nil
}

func (f *fakeRepo) GetEnvironment(_ context.Context, envID int64) (*domain.Environment, error) {
	e, ok := f.envs[envID]
	if !ok {
		return nil, errcode.ErrNotFound
	}
	return f.withTags(e), nil
}

func (f *fakeRepo) CreateEnvironment(_ context.Context, env *domain.Environment) error {
	for _, e := range f.envs {
		if e.Code == env.Code {
			return errcode.ErrConflict
		}
	}
	f.nextEnv++
	env.ID = f.nextEnv
	env.Version = 1
	env.CreatedAt = time.Now()
	env.UpdatedAt = env.CreatedAt
	cp := *env
	cp.TagIDs = nil
	f.envs[env.ID] = &cp
	delete(f.envTags, env.ID)
	return nil
}

func (f *fakeRepo) UpdateEnvironment(_ context.Context, env *domain.Environment, version int64) error {
	cur, ok := f.envs[env.ID]
	if !ok {
		return errcode.ErrNotFound
	}
	if version <= 0 || cur.Version != version {
		return errcode.ErrConcurrent
	}
	for id, other := range f.envs {
		if id != env.ID && other.Code == env.Code {
			return errcode.ErrConflict
		}
	}
	f.updateEnvCalls++
	cur.Name, cur.Code, cur.CategoryID = env.Name, env.Code, env.CategoryID
	cur.Type, cur.Hosts, cur.OS = env.Type, append([]string(nil), env.Hosts...), env.OS
	cur.SshHostID, cur.CertID, cur.Description = env.SshHostID, env.CertID, env.Description
	cur.Version++
	cur.UpdatedAt = time.Now()
	env.Version = cur.Version
	return nil
}

func (f *fakeRepo) UpdateEnvironmentStatus(_ context.Context, envID int64, status string) error {
	e, ok := f.envs[envID]
	if !ok {
		return errcode.ErrNotFound
	}
	if e.Status == domain.EnvStatusTesting {
		return ErrEnvOccupied
	}
	e.Status = status
	e.Version++
	e.UpdatedAt = time.Now()
	f.statusCalls[envID] = status
	return nil
}

func (f *fakeRepo) SoftDeleteEnvironment(_ context.Context, envID int64) error {
	e, ok := f.envs[envID]
	if !ok {
		return errcode.ErrNotFound
	}
	if e.Status == domain.EnvStatusTesting {
		return ErrEnvOccupied
	}
	f.deletedEnvs = append(f.deletedEnvs, envID)
	delete(f.envs, envID)
	delete(f.envTags, envID)
	return nil
}

func (f *fakeRepo) ReplaceEnvironmentTags(_ context.Context, envID int64, tagIDs []int64) error {
	if _, ok := f.envs[envID]; !ok {
		return errcode.ErrNotFound
	}
	f.tagReplaceCalls++
	f.lastTagIDs = append([]int64(nil), tagIDs...)
	f.envTags[envID] = append([]int64(nil), tagIDs...)
	return nil
}

// withTags 返回带 TagIDs 的环境副本。
func (f *fakeRepo) withTags(e *domain.Environment) *domain.Environment {
	cp := *e
	cp.Hosts = append([]string(nil), e.Hosts...)
	cp.TagIDs = append([]int64(nil), f.envTags[e.ID]...)
	return &cp
}

// containsAll 判断 have 是否包含 want 的全部元素（标签筛选取交集）。
func containsAll(have, want []int64) bool {
	set := make(map[int64]bool, len(have))
	for _, id := range have {
		set[id] = true
	}
	for _, id := range want {
		if !set[id] {
			return false
		}
	}
	return true
}

// paginate 按页切片（页码从 1 开始）。
func paginate(list []domain.Environment, page, pageSize int) []domain.Environment {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	start := (page - 1) * pageSize
	if start >= len(list) {
		return []domain.Environment{}
	}
	end := start + pageSize
	if end > len(list) {
		end = len(list)
	}
	return list[start:end]
}

// newTestService 构造带假仓储的 service。
func newTestService() (*Service, *fakeRepo) {
	repo := newFakeRepo()
	return New(repo), repo
}

// addCategory 写入分类。
func addCategory(repo *fakeRepo, id int64, name string) *domain.EnvironmentCategory {
	c := &domain.EnvironmentCategory{ID: id, Name: name, Version: 1}
	repo.categories[id] = c
	return c
}

// addTag 写入标签。
func addTag(repo *fakeRepo, id int64, name string) *domain.Tag {
	t := &domain.Tag{ID: id, Name: name, Color: "#2563eb", Version: 1}
	repo.tags[id] = t
	return t
}

// addEnv 写入环境（含标签关联）。
func addEnv(repo *fakeRepo, id int64, name, code string, categoryID int64, status string, tagIDs ...int64) *domain.Environment {
	e := &domain.Environment{
		ID: id, Name: name, Code: code, CategoryID: categoryID, Type: domain.EnvTypeSingle,
		Hosts: []string{"10.0.0.1"}, Status: status, Version: 1,
	}
	repo.envs[id] = e
	repo.envTags[id] = append([]int64(nil), tagIDs...)
	return e
}
