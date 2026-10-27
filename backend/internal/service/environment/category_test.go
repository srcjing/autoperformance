package environment

import (
	"context"
	"errors"
	"testing"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
)

// TestDeleteCategoryReferenced 分类被环境引用时禁止删除（INV-6，21002），且不落库。
func TestDeleteCategoryReferenced(t *testing.T) {
	svc, repo := newTestService()
	addCategory(repo, 11, "生产集群")
	addEnv(repo, 310, "生产环境", "prod", 11, domain.EnvStatusPending)

	err := svc.DeleteCategory(context.Background(), 11)
	if !errors.Is(err, ErrCategoryRefed) {
		t.Fatalf("want 21002, got %v", err)
	}
	if len(repo.deletedCategories) != 0 {
		t.Fatalf("被引用的分类不得删除: %v", repo.deletedCategories)
	}
	if _, ok := repo.categories[11]; !ok {
		t.Fatal("分类不应从仓储移除")
	}
}

// TestDeleteCategorySucceedsWhenUnreferenced 无引用分类逻辑删除成功。
func TestDeleteCategorySucceedsWhenUnreferenced(t *testing.T) {
	svc, repo := newTestService()
	addCategory(repo, 12, "临时分类")

	if err := svc.DeleteCategory(context.Background(), 12); err != nil {
		t.Fatalf("删除分类失败: %v", err)
	}
	if len(repo.deletedCategories) != 1 || repo.deletedCategories[0] != 12 {
		t.Fatalf("应软删除分类 12: %v", repo.deletedCategories)
	}
}

// TestDeleteCategoryNotFound 不存在的分类 → 10301。
func TestDeleteCategoryNotFound(t *testing.T) {
	svc, _ := newTestService()
	if err := svc.DeleteCategory(context.Background(), 999); !errors.Is(err, errcode.ErrNotFound) {
		t.Fatalf("want 10301, got %v", err)
	}
}

// TestCreateCategoryValidation 分类名称必填/长度上限/重名拦截（10302）。
func TestCreateCategoryValidation(t *testing.T) {
	svc, repo := newTestService()
	addCategory(repo, 13, "已存在")
	ctx := context.Background()

	if _, err := svc.CreateCategory(ctx, CategoryInput{Name: "  "}); !errors.Is(err, errcode.ErrParamMiss) {
		t.Fatalf("空名称 want 10002, got %v", err)
	}
	longName := ""
	for i := 0; i < 65; i++ {
		longName += "分"
	}
	if _, err := svc.CreateCategory(ctx, CategoryInput{Name: longName}); !errors.Is(err, errcode.ErrParamFmt) {
		t.Fatalf("超长名称 want 10003, got %v", err)
	}
	if _, err := svc.CreateCategory(ctx, CategoryInput{Name: "已存在"}); !errors.Is(err, errcode.ErrConflict) {
		t.Fatalf("重名 want 10302, got %v", err)
	}

	view, err := svc.CreateCategory(ctx, CategoryInput{Name: " 单机 ", Description: "单机环境"})
	if err != nil {
		t.Fatalf("新建分类失败: %v", err)
	}
	if view.Name != "单机" {
		t.Fatalf("名称应去除首尾空白, got %q", view.Name)
	}
	if view.Version != 1 || view.ID == 0 {
		t.Fatalf("返回视图异常: %+v", view)
	}
}

// TestListCategories 列表按 ID 稳定排序。
func TestListCategories(t *testing.T) {
	svc, repo := newTestService()
	addCategory(repo, 21, "A")
	addCategory(repo, 20, "B")

	list, err := svc.ListCategories(context.Background())
	if err != nil {
		t.Fatalf("查询分类失败: %v", err)
	}
	if len(list) != 2 || list[0].ID != 20 || list[1].ID != 21 {
		t.Fatalf("分类列表异常: %+v", list)
	}
}

// TestUpdateCategory 修改分类；不存在 → 10301。
func TestUpdateCategory(t *testing.T) {
	svc, repo := newTestService()
	addCategory(repo, 31, "旧名称")
	ctx := context.Background()

	view, err := svc.UpdateCategory(ctx, 31, CategoryInput{Name: "新名称", Description: "说明"})
	if err != nil {
		t.Fatalf("修改分类失败: %v", err)
	}
	if view.Name != "新名称" || view.Description != "说明" {
		t.Fatalf("修改结果异常: %+v", view)
	}

	if _, err := svc.UpdateCategory(ctx, 999, CategoryInput{Name: "x"}); !errors.Is(err, errcode.ErrNotFound) {
		t.Fatalf("want 10301, got %v", err)
	}
}
