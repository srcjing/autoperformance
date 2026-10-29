package product

import (
	"context"
	"errors"
	"testing"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
)

// validProductInput 构造合法的产品输入。
func validProductInput() ProductInput {
	return ProductInput{
		Name:           "网关服务",
		Code:           "gateway",
		ProductVersion: "v1.2.3",
		Platform:       "Linux-x86",
		Description:    "待测产品",
	}
}

// TestCreateProductValidatesInput 新建产品：必填校验、编码唯一（22001）、初始状态 enabled。
func TestCreateProductValidatesInput(t *testing.T) {
	svc, repo := newTestService()
	addProduct(repo, 100, "已存在", "dup-code", domain.ProductStatusEnabled)
	ctx := context.Background()

	cases := []struct {
		name string
		mut  func(in *ProductInput)
		want error
	}{
		{"缺名称", func(in *ProductInput) { in.Name = " " }, errcode.ErrParamMiss},
		{"缺编码", func(in *ProductInput) { in.Code = "" }, errcode.ErrParamMiss},
		{"缺版本", func(in *ProductInput) { in.ProductVersion = "" }, errcode.ErrParamMiss},
		{"名称过长", func(in *ProductInput) { in.Name = string(make([]rune, maxNameLen+1)) }, errcode.ErrParamFmt},
		{"编码重复", func(in *ProductInput) { in.Code = "dup-code" }, ErrCodeExists},
	}
	for _, c := range cases {
		in := validProductInput()
		c.mut(&in)
		if _, err := svc.CreateProduct(ctx, in); !errors.Is(err, c.want) {
			t.Errorf("%s: want %v, got %v", c.name, c.want, err)
		}
	}
	if len(repo.products) != 1 {
		t.Fatalf("校验失败不应落库，产品数 = %d", len(repo.products))
	}

	view, err := svc.CreateProduct(ctx, validProductInput())
	if err != nil {
		t.Fatalf("创建产品失败: %v", err)
	}
	if view.Status != domain.ProductStatusEnabled {
		t.Fatalf("新产品初始状态应为 enabled, got %s", view.Status)
	}
	if view.Version != 1 || view.ID == 0 || view.ProductVersion != "v1.2.3" {
		t.Fatalf("返回视图异常: %+v", view)
	}
}

// TestDeleteProductWithInterfaces 产品下存在接口时禁止删除（22002），并保持记录不被删除（INV-6）。
func TestDeleteProductWithInterfaces(t *testing.T) {
	svc, repo := newTestService()
	addProduct(repo, 100, "网关服务", "gateway", domain.ProductStatusEnabled)
	addInterface(repo, 200, 100, "健康检查", domain.InterfaceStatusEnabled, nil)
	ctx := context.Background()

	if err := svc.DeleteProduct(ctx, 100); !errors.Is(err, ErrProductHasInterfaces) {
		t.Fatalf("有接口的产品删除 want 22002, got %v", err)
	}
	if len(repo.deletedProducts) != 0 {
		t.Fatal("校验失败时不得删除产品")
	}

	// 接口逻辑删除后允许删除产品。
	delete(repo.interfaces, 200)
	if err := svc.DeleteProduct(ctx, 100); err != nil {
		t.Fatalf("无接口的产品应可删除: %v", err)
	}
	if len(repo.deletedProducts) != 1 || repo.deletedProducts[0] != 100 {
		t.Fatalf("应为逻辑删除: %v", repo.deletedProducts)
	}

	if err := svc.DeleteProduct(ctx, 999); !errors.Is(err, errcode.ErrNotFound) {
		t.Fatalf("产品不存在 want 10301, got %v", err)
	}
}

// TestUpdateProductOptimisticLock 全量修改携带 version：缺失 → 10002，过期 → 10402，命中 → 版本递增。
func TestUpdateProductOptimisticLock(t *testing.T) {
	svc, repo := newTestService()
	addProduct(repo, 100, "网关服务", "gateway", domain.ProductStatusEnabled)
	addProduct(repo, 101, "支付服务", "payment", domain.ProductStatusEnabled)
	ctx := context.Background()

	in := validProductInput()
	in.Code = "gateway"

	if _, err := svc.UpdateProduct(ctx, 100, in); !errors.Is(err, errcode.ErrParamMiss) {
		t.Fatalf("缺 version want 10002, got %v", err)
	}

	in.Version = 9
	if _, err := svc.UpdateProduct(ctx, 100, in); !errors.Is(err, errcode.ErrConcurrent) {
		t.Fatalf("版本过期 want 10402, got %v", err)
	}
	if repo.updateCalls != 0 {
		t.Fatal("并发冲突时不应写库")
	}

	in.Version = 1
	in.Name = "网关服务-改"
	view, err := svc.UpdateProduct(ctx, 100, in)
	if err != nil {
		t.Fatalf("修改产品失败: %v", err)
	}
	if view.Version != 2 || view.Name != "网关服务-改" {
		t.Fatalf("修改结果异常: %+v", view)
	}

	// 编码与其他产品冲突 → 22001。
	in.Version = 2
	in.Code = "payment"
	if _, err := svc.UpdateProduct(ctx, 100, in); !errors.Is(err, ErrCodeExists) {
		t.Fatalf("编码重复 want 22001, got %v", err)
	}
}

// TestProductStatusTransitions 产品状态机：enabled ↔ disabled；同状态/非法取值被拒。
func TestProductStatusTransitions(t *testing.T) {
	svc, repo := newTestService()
	addProduct(repo, 100, "网关服务", "gateway", domain.ProductStatusEnabled)
	ctx := context.Background()

	if err := svc.UpdateStatus(ctx, 100, "offline"); !errors.Is(err, errcode.ErrParamFmt) {
		t.Fatalf("非法状态 want 10003, got %v", err)
	}
	if err := svc.UpdateStatus(ctx, 100, domain.ProductStatusEnabled); !errors.Is(err, errcode.ErrState) {
		t.Fatalf("同状态重复设置 want 10401, got %v", err)
	}
	if err := svc.UpdateStatus(ctx, 100, domain.ProductStatusDisabled); err != nil {
		t.Fatalf("停用失败: %v", err)
	}
	if repo.statusCalls[100] != domain.ProductStatusDisabled {
		t.Fatalf("状态未写入: %v", repo.statusCalls)
	}
	if err := svc.UpdateStatus(ctx, 100, domain.ProductStatusEnabled); err != nil {
		t.Fatalf("重新启用失败: %v", err)
	}
	if err := svc.UpdateStatus(ctx, 999, domain.ProductStatusDisabled); !errors.Is(err, errcode.ErrNotFound) {
		t.Fatalf("产品不存在 want 10301, got %v", err)
	}
}

// TestListProductsFilters 关键字/版本/平台/状态组合筛选 + 接口数统计 + 分页。
func TestListProductsFilters(t *testing.T) {
	svc, repo := newTestService()
	addProduct(repo, 100, "网关服务", "gateway", domain.ProductStatusEnabled)
	addProduct(repo, 101, "支付服务", "payment", domain.ProductStatusDisabled)
	addProduct(repo, 102, "网关代理", "gateway-proxy", domain.ProductStatusEnabled)
	addInterface(repo, 200, 100, "健康检查", domain.InterfaceStatusEnabled, nil)
	addInterface(repo, 201, 100, "下单", domain.InterfaceStatusEnabled, nil)
	ctx := context.Background()

	page, err := svc.ListProducts(ctx, ListProductsQuery{})
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if page.Total != 3 || page.Page != 1 || page.PageSize != DefaultPageSize {
		t.Fatalf("分页默认值异常: %+v", page)
	}

	page, _ = svc.ListProducts(ctx, ListProductsQuery{Keyword: "网关"})
	if page.Total != 2 {
		t.Fatalf("关键字过滤异常: total=%d", page.Total)
	}

	page, _ = svc.ListProducts(ctx, ListProductsQuery{Status: domain.ProductStatusDisabled})
	if page.Total != 1 || page.List[0].ID != 101 {
		t.Fatalf("状态过滤异常: %+v", page.List)
	}

	page, _ = svc.ListProducts(ctx, ListProductsQuery{Page: 2, PageSize: 2})
	if len(page.List) != 1 || page.Total != 3 {
		t.Fatalf("分页切片异常: len=%d total=%d", len(page.List), page.Total)
	}

	detail, err := svc.GetProduct(ctx, 100)
	if err != nil {
		t.Fatalf("查询详情失败: %v", err)
	}
	if detail.InterfaceCount != 2 {
		t.Fatalf("接口数统计异常: %+v", detail)
	}

	if _, err := svc.ListProducts(ctx, ListProductsQuery{Status: "offline"}); !errors.Is(err, errcode.ErrParamFmt) {
		t.Fatalf("非法状态过滤 want 10003, got %v", err)
	}
	if _, err := svc.GetProduct(ctx, 999); !errors.Is(err, errcode.ErrNotFound) {
		t.Fatalf("want 10301, got %v", err)
	}
}
