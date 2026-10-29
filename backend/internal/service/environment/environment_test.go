package environment

import (
	"context"
	"errors"
	"testing"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
)

// validEnvInput 构造合法的环境输入。
func validEnvInput() EnvironmentInput {
	return EnvironmentInput{
		Name:       "测试环境",
		Code:       "test-env",
		CategoryID: 10,
		Type:       domain.EnvTypeCluster,
		Hosts:      []string{"10.0.0.1", " 10.0.0.2 "},
		OS:         "Linux",
	}
}

// TestCreateEnvironmentValidatesCategoryAndCode 新建环境：分类必填/存在、编码唯一（21001）、
// 标签须预定义、主机列表必填、类型枚举合法、初始状态 pending。
func TestCreateEnvironmentValidatesCategoryAndCode(t *testing.T) {
	svc, repo := newTestService()
	addCategory(repo, 10, "单机")
	addTag(repo, 201, "内网")
	addEnv(repo, 310, "已存在", "dup-code", 10, domain.EnvStatusPending)
	ctx := context.Background()

	cases := []struct {
		name string
		mut  func(in *EnvironmentInput)
		want error
	}{
		{"缺分类", func(in *EnvironmentInput) { in.CategoryID = 0 }, errcode.ErrParamMiss},
		{"分类不存在", func(in *EnvironmentInput) { in.CategoryID = 999 }, errcode.ErrNotFound},
		{"标签不存在", func(in *EnvironmentInput) { in.TagIDs = []int64{999} }, errcode.ErrNotFound},
		{"编码重复", func(in *EnvironmentInput) { in.Code = "dup-code" }, ErrCodeExists},
		{"缺名称", func(in *EnvironmentInput) { in.Name = " " }, errcode.ErrParamMiss},
		{"缺编码", func(in *EnvironmentInput) { in.Code = "" }, errcode.ErrParamMiss},
		{"缺主机", func(in *EnvironmentInput) { in.Hosts = nil }, errcode.ErrParamMiss},
		{"空主机项", func(in *EnvironmentInput) { in.Hosts = []string{"  "} }, errcode.ErrParamMiss},
		{"类型非法", func(in *EnvironmentInput) { in.Type = "vm" }, errcode.ErrParamFmt},
	}
	for _, c := range cases {
		in := validEnvInput()
		c.mut(&in)
		if _, err := svc.CreateEnvironment(ctx, in); !errors.Is(err, c.want) {
			t.Errorf("%s: want %v, got %v", c.name, c.want, err)
		}
	}
	if len(repo.envs) != 1 {
		t.Fatalf("校验失败不应落库，环境数 = %d", len(repo.envs))
	}

	in := validEnvInput()
	in.TagIDs = []int64{201, 201}
	view, err := svc.CreateEnvironment(ctx, in)
	if err != nil {
		t.Fatalf("新建环境失败: %v", err)
	}
	if view.Status != domain.EnvStatusPending {
		t.Fatalf("新建环境初始状态应为 pending, got %s", view.Status)
	}
	if view.Version != 1 || view.ID == 0 {
		t.Fatalf("返回视图异常: %+v", view)
	}
	if got := repo.envTags[view.ID]; len(got) != 1 || got[0] != 201 {
		t.Fatalf("标签应去重绑定: %v", got)
	}
	if len(view.Hosts) != 2 || view.Hosts[1] != "10.0.0.2" {
		t.Fatalf("主机列表应去空白: %v", view.Hosts)
	}
	if len(view.Tags) != 1 || view.Tags[0].Name != "内网" {
		t.Fatalf("返回视图应含标签: %+v", view.Tags)
	}
	if view.CategoryName != "单机" {
		t.Fatalf("返回视图应含分类名称: %+v", view)
	}
}

// TestBindTags 标签绑定为全量替换；未预定义标签拒绝且不落库。
func TestBindTags(t *testing.T) {
	svc, repo := newTestService()
	addCategory(repo, 10, "单机")
	addTag(repo, 201, "内网")
	addTag(repo, 202, "高性能")
	addEnv(repo, 310, "环境A", "env-a", 10, domain.EnvStatusPending, 201)
	ctx := context.Background()

	if err := svc.BindTags(ctx, 310, []int64{202, 202}); err != nil {
		t.Fatalf("绑定标签失败: %v", err)
	}
	if repo.tagReplaceCalls != 1 {
		t.Fatalf("应调用一次全量替换，got %d", repo.tagReplaceCalls)
	}
	if len(repo.lastTagIDs) != 1 || repo.lastTagIDs[0] != 202 {
		t.Fatalf("标签应去重并全量替换: %v", repo.lastTagIDs)
	}

	before := repo.tagReplaceCalls
	if err := svc.BindTags(ctx, 310, []int64{201, 999}); !errors.Is(err, errcode.ErrNotFound) {
		t.Fatalf("want 10301, got %v", err)
	}
	if repo.tagReplaceCalls != before {
		t.Fatal("校验失败时不应写库")
	}
	if err := svc.BindTags(ctx, 999, nil); !errors.Is(err, errcode.ErrNotFound) {
		t.Fatalf("环境不存在 want 10301, got %v", err)
	}
}

// TestDisableBusyEnvironment 环境 testing 时禁止停用/删除（21004），状态与数据不变。
func TestDisableBusyEnvironment(t *testing.T) {
	svc, repo := newTestService()
	addCategory(repo, 10, "单机")
	addEnv(repo, 310, "压测中", "busy", 10, domain.EnvStatusTesting)
	ctx := context.Background()

	if err := svc.UpdateStatus(ctx, 310, domain.EnvStatusDisabled); !errors.Is(err, ErrEnvOccupied) {
		t.Fatalf("停用测试中环境 want 21004, got %v", err)
	}
	if _, ok := repo.statusCalls[310]; ok {
		t.Fatal("测试中环境状态不得被修改")
	}
	if err := svc.DeleteEnvironment(ctx, 310); !errors.Is(err, ErrEnvOccupied) {
		t.Fatalf("删除测试中环境 want 21004, got %v", err)
	}
	if len(repo.deletedEnvs) != 0 {
		t.Fatal("测试中环境不得被删除")
	}

	addEnv(repo, 311, "空闲环境", "idle", 10, domain.EnvStatusPending)
	if err := svc.UpdateStatus(ctx, 311, domain.EnvStatusDisabled); err != nil {
		t.Fatalf("停用空闲环境失败: %v", err)
	}
	if repo.statusCalls[311] != domain.EnvStatusDisabled {
		t.Fatalf("状态未写入: %v", repo.statusCalls)
	}
}

// TestOptimisticLockConflict 全量修改携带 version：缺失 → 10002，过期 → 10402，命中 → 版本递增。
func TestOptimisticLockConflict(t *testing.T) {
	svc, repo := newTestService()
	addCategory(repo, 10, "单机")
	addEnv(repo, 310, "环境A", "env-a", 10, domain.EnvStatusPending)
	ctx := context.Background()

	in := validEnvInput()
	in.Code = "env-a"

	in.Version = 0
	if _, err := svc.UpdateEnvironment(ctx, 310, in); !errors.Is(err, errcode.ErrParamMiss) {
		t.Fatalf("缺 version want 10002, got %v", err)
	}

	in.Version = 9
	if _, err := svc.UpdateEnvironment(ctx, 310, in); !errors.Is(err, errcode.ErrConcurrent) {
		t.Fatalf("版本过期 want 10402, got %v", err)
	}
	if repo.updateEnvCalls != 0 {
		t.Fatal("并发冲突时不应写库")
	}

	in.Version = 1
	in.Name = "环境A-改"
	view, err := svc.UpdateEnvironment(ctx, 310, in)
	if err != nil {
		t.Fatalf("修改环境失败: %v", err)
	}
	if view.Version != 2 {
		t.Fatalf("版本应递增到 2, got %d", view.Version)
	}
	if view.Name != "环境A-改" || view.Status != domain.EnvStatusPending {
		t.Fatalf("修改结果异常: %+v", view)
	}

	// 旧版本号再次提交 → 10402（防止并发覆盖用户改动，DES-T-001）。
	if _, err := svc.UpdateEnvironment(ctx, 310, in); !errors.Is(err, errcode.ErrConcurrent) {
		t.Fatalf("重复提交旧版本 want 10402, got %v", err)
	}
}

// TestUpdateEnvironmentUniqueCode 修改时编码与其他环境冲突 → 21001。
func TestUpdateEnvironmentUniqueCode(t *testing.T) {
	svc, repo := newTestService()
	addCategory(repo, 10, "单机")
	addEnv(repo, 310, "环境A", "env-a", 10, domain.EnvStatusPending)
	addEnv(repo, 311, "环境B", "env-b", 10, domain.EnvStatusPending)

	in := validEnvInput()
	in.Code = "env-b"
	in.Version = 1
	if _, err := svc.UpdateEnvironment(context.Background(), 310, in); !errors.Is(err, ErrCodeExists) {
		t.Fatalf("want 21001, got %v", err)
	}
}

// TestEnvironmentStatusTransitions 手动状态机仅 pending ↔ disabled；testing 由任务驱动。
func TestEnvironmentStatusTransitions(t *testing.T) {
	svc, repo := newTestService()
	addCategory(repo, 10, "单机")
	addEnv(repo, 310, "待测试", "p", 10, domain.EnvStatusPending)
	addEnv(repo, 311, "已下线", "d", 10, domain.EnvStatusDisabled)
	addEnv(repo, 312, "测试中", "t", 10, domain.EnvStatusTesting)
	ctx := context.Background()

	if err := svc.UpdateStatus(ctx, 310, domain.EnvStatusTesting); !errors.Is(err, errcode.ErrParamFmt) {
		t.Fatalf("PATCH 不允许手动置为 testing, want 10003, got %v", err)
	}
	if err := svc.UpdateStatus(ctx, 310, "offline"); !errors.Is(err, errcode.ErrParamFmt) {
		t.Fatalf("非法状态 want 10003, got %v", err)
	}
	if err := svc.UpdateStatus(ctx, 311, domain.EnvStatusPending); err != nil {
		t.Fatalf("重新启用失败: %v", err)
	}
	if err := svc.UpdateStatus(ctx, 311, domain.EnvStatusPending); !errors.Is(err, errcode.ErrState) {
		t.Fatalf("同状态重复设置 want 10401, got %v", err)
	}
	if err := svc.UpdateStatus(ctx, 312, domain.EnvStatusPending); !errors.Is(err, errcode.ErrState) {
		t.Fatalf("testing→pending 由任务编排驱动, want 10401, got %v", err)
	}
	if err := svc.UpdateStatus(ctx, 999, domain.EnvStatusDisabled); !errors.Is(err, errcode.ErrNotFound) {
		t.Fatalf("环境不存在 want 10301, got %v", err)
	}
}

// TestListEnvironmentsFilters 按关键字/分类/状态/标签（取交集）组合筛选 + 分页。
func TestListEnvironmentsFilters(t *testing.T) {
	svc, repo := newTestService()
	addCategory(repo, 10, "单机")
	addCategory(repo, 20, "集群")
	addTag(repo, 201, "内网")
	addTag(repo, 202, "高性能")
	addEnv(repo, 310, "内网单机", "env-intra", 10, domain.EnvStatusPending, 201)
	addEnv(repo, 311, "高性能集群", "env-perf", 20, domain.EnvStatusTesting, 201, 202)
	addEnv(repo, 312, "已下线环境", "env-off", 20, domain.EnvStatusDisabled, 202)
	addEnv(repo, 313, "另一单机", "env-intra-2", 10, domain.EnvStatusPending)
	ctx := context.Background()

	page, err := svc.ListEnvironments(ctx, ListEnvironmentsQuery{})
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if page.Total != 4 || page.Page != 1 || page.PageSize != 20 {
		t.Fatalf("分页默认值异常: %+v", page)
	}

	page, _ = svc.ListEnvironments(ctx, ListEnvironmentsQuery{Keyword: "高性能"})
	if page.Total != 1 || page.List[0].ID != 311 {
		t.Fatalf("关键字过滤异常: %+v", page.List)
	}

	page, _ = svc.ListEnvironments(ctx, ListEnvironmentsQuery{CategoryID: 20})
	if page.Total != 2 {
		t.Fatalf("分类过滤异常: total=%d", page.Total)
	}

	page, _ = svc.ListEnvironments(ctx, ListEnvironmentsQuery{Status: domain.EnvStatusPending})
	if page.Total != 2 {
		t.Fatalf("状态过滤异常: total=%d", page.Total)
	}

	page, _ = svc.ListEnvironments(ctx, ListEnvironmentsQuery{TagIDs: []int64{201, 202}})
	if page.Total != 1 || page.List[0].ID != 311 {
		t.Fatalf("标签取交集异常: %+v", page.List)
	}

	page, _ = svc.ListEnvironments(ctx, ListEnvironmentsQuery{Page: 2, PageSize: 3})
	if len(page.List) != 1 || page.Total != 4 {
		t.Fatalf("分页切片异常: len=%d total=%d", len(page.List), page.Total)
	}

	if _, err := svc.ListEnvironments(ctx, ListEnvironmentsQuery{Status: "offline"}); !errors.Is(err, errcode.ErrParamFmt) {
		t.Fatalf("非法状态过滤 want 10003, got %v", err)
	}
}

// TestGetEnvironmentDetail 详情返回标签/分类名称；不存在 → 10301。
func TestGetEnvironmentDetail(t *testing.T) {
	svc, repo := newTestService()
	addCategory(repo, 10, "单机")
	addTag(repo, 201, "内网")
	addEnv(repo, 310, "环境A", "env-a", 10, domain.EnvStatusPending, 201)
	ctx := context.Background()

	view, err := svc.GetEnvironment(ctx, 310)
	if err != nil {
		t.Fatalf("查询详情失败: %v", err)
	}
	if view.Code != "env-a" || view.CategoryName != "单机" || len(view.Tags) != 1 {
		t.Fatalf("详情异常: %+v", view)
	}
	if _, err := svc.GetEnvironment(ctx, 999); !errors.Is(err, errcode.ErrNotFound) {
		t.Fatalf("want 10301, got %v", err)
	}
}
