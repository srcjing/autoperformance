package environment

import (
	"context"
	"errors"
	"testing"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
)

// TestTagEnvironmentCount 标签列表返回关联环境数（环境侧打标签后双向可查）。
func TestTagEnvironmentCount(t *testing.T) {
	svc, repo := newTestService()
	addCategory(repo, 10, "单机")
	addTag(repo, 201, "内网")
	addTag(repo, 202, "高性能")
	addEnv(repo, 310, "环境A", "env-a", 10, domain.EnvStatusPending, 201, 202)
	addEnv(repo, 311, "环境B", "env-b", 10, domain.EnvStatusPending, 201)

	list, err := svc.ListTags(context.Background(), ListTagsQuery{})
	if err != nil {
		t.Fatalf("查询标签失败: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("标签数量 = %d, want 2", len(list))
	}
	counts := map[int64]int64{}
	for _, tag := range list {
		counts[tag.ID] = tag.EnvironmentCount
	}
	if counts[201] != 2 || counts[202] != 1 {
		t.Fatalf("关联环境数异常: %v", counts)
	}
}

// TestTagKeywordFilter 标签关键字过滤（名称/说明）。
func TestTagKeywordFilter(t *testing.T) {
	svc, repo := newTestService()
	addTag(repo, 211, "内网")
	addTag(repo, 212, "公网")
	ctx := context.Background()

	list, err := svc.ListTags(ctx, ListTagsQuery{Keyword: "内"})
	if err != nil {
		t.Fatalf("查询标签失败: %v", err)
	}
	if len(list) != 1 || list[0].ID != 211 {
		t.Fatalf("关键字过滤异常: %+v", list)
	}
}

// TestDeleteTagReferenced 标签被环境引用时禁止删除（INV-6，21003）。
func TestDeleteTagReferenced(t *testing.T) {
	svc, repo := newTestService()
	addCategory(repo, 10, "单机")
	addTag(repo, 221, "内网")
	addEnv(repo, 310, "环境A", "env-a", 10, domain.EnvStatusPending, 221)

	err := svc.DeleteTag(context.Background(), 221)
	if !errors.Is(err, ErrTagRefed) {
		t.Fatalf("want 21003, got %v", err)
	}
	if len(repo.deletedTags) != 0 {
		t.Fatalf("被引用的标签不得删除: %v", repo.deletedTags)
	}
}

// TestDeleteTagUnreferenced 无引用标签逻辑删除成功；不存在 → 10301。
func TestDeleteTagUnreferenced(t *testing.T) {
	svc, repo := newTestService()
	addTag(repo, 231, "临时标签")
	ctx := context.Background()

	if err := svc.DeleteTag(ctx, 231); err != nil {
		t.Fatalf("删除标签失败: %v", err)
	}
	if len(repo.deletedTags) != 1 || repo.deletedTags[0] != 231 {
		t.Fatalf("应软删除标签 231: %v", repo.deletedTags)
	}
	if err := svc.DeleteTag(ctx, 999); !errors.Is(err, errcode.ErrNotFound) {
		t.Fatalf("want 10301, got %v", err)
	}
}

// TestCreateTagValidation 标签名称必填/重名/颜色格式校验。
func TestCreateTagValidation(t *testing.T) {
	svc, repo := newTestService()
	addTag(repo, 241, "内网")
	ctx := context.Background()

	cases := []struct {
		name string
		in   TagInput
		want error
	}{
		{"空名称", TagInput{Name: " "}, errcode.ErrParamMiss},
		{"重名", TagInput{Name: "内网"}, errcode.ErrConflict},
		{"颜色非法", TagInput{Name: "压测", Color: "red"}, errcode.ErrParamFmt},
	}
	for _, c := range cases {
		if _, err := svc.CreateTag(ctx, c.in); !errors.Is(err, c.want) {
			t.Errorf("%s: want %v, got %v", c.name, c.want, err)
		}
	}

	view, err := svc.CreateTag(ctx, TagInput{Name: "压测", Color: "#10B981", Description: "性能压测"})
	if err != nil {
		t.Fatalf("新建标签失败: %v", err)
	}
	if view.ID == 0 || view.Color != "#10B981" {
		t.Fatalf("返回视图异常: %+v", view)
	}
}

// TestUpdateTag 修改标签；不存在 → 10301。
func TestUpdateTag(t *testing.T) {
	svc, repo := newTestService()
	addTag(repo, 251, "旧标签")
	ctx := context.Background()

	view, err := svc.UpdateTag(ctx, 251, TagInput{Name: "新标签", Color: "#000000"})
	if err != nil {
		t.Fatalf("修改标签失败: %v", err)
	}
	if view.Name != "新标签" || view.Color != "#000000" {
		t.Fatalf("修改结果异常: %+v", view)
	}
	if _, err := svc.UpdateTag(ctx, 999, TagInput{Name: "x"}); !errors.Is(err, errcode.ErrNotFound) {
		t.Fatalf("want 10301, got %v", err)
	}
}

// TestTagEnvironmentsPage 标签反查关联环境（分页）；标签不存在 → 10301。
func TestTagEnvironmentsPage(t *testing.T) {
	svc, repo := newTestService()
	addCategory(repo, 10, "单机")
	addTag(repo, 261, "内网")
	for i := int64(1); i <= 3; i++ {
		addEnv(repo, 300+i, "环境"+string(rune('A'+i-1)), "env-"+string(rune('a'+i-1)), 10, domain.EnvStatusPending, 261)
	}
	ctx := context.Background()

	page, err := svc.TagEnvironments(ctx, 261, 1, 2)
	if err != nil {
		t.Fatalf("反查失败: %v", err)
	}
	if page.Total != 3 || len(page.List) != 2 || page.PageSize != 2 {
		t.Fatalf("分页结果异常: total=%d len=%d pageSize=%d", page.Total, len(page.List), page.PageSize)
	}
	if len(page.List[0].Tags) != 1 || page.List[0].Tags[0].Name != "内网" {
		t.Fatalf("环境视图应含标签: %+v", page.List[0].Tags)
	}

	if _, err := svc.TagEnvironments(ctx, 999, 1, 20); !errors.Is(err, errcode.ErrNotFound) {
		t.Fatalf("want 10301, got %v", err)
	}
}
