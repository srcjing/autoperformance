package product

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"auto_performance/internal/crypto"
	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
)

// validInterfaceInput 构造合法的接口输入（含一个普通头与一个认证头）。
func validInterfaceInput() InterfaceInput {
	return InterfaceInput{
		Name:   "创建订单",
		Method: domain.MethodPost,
		URL:    "https://api.example.com/orders",
		Headers: map[string]string{
			"Content-Type":  "application/json",
			"Authorization": "Bearer super-secret-token",
		},
		Body:     `{"sku":"A-1","qty":1}`,
		Timeouts: domain.InterfaceTimeouts{ConnectMS: 1000, ReadMS: 5000},
	}
}

// TestCreateInterfaceValidatesInput 接口新建：产品必须存在、地址必须 http(s)、方法/参数模板/超时合法。
func TestCreateInterfaceValidatesInput(t *testing.T) {
	svc, repo := newTestService()
	addProduct(repo, 100, "网关服务", "gateway", domain.ProductStatusEnabled)
	ctx := context.Background()

	cases := []struct {
		name string
		mut  func(in *InterfaceInput)
		want error
	}{
		{"缺名称", func(in *InterfaceInput) { in.Name = "" }, errcode.ErrParamMiss},
		{"缺地址", func(in *InterfaceInput) { in.URL = " " }, errcode.ErrParamMiss},
		{"地址非 http(s)", func(in *InterfaceInput) { in.URL = "ftp://api.example.com" }, errcode.ErrParamFmt},
		{"方法非法", func(in *InterfaceInput) { in.Method = "TRACE" }, errcode.ErrParamFmt},
		{"参数模板非 JSON", func(in *InterfaceInput) { in.Body = "{not-json" }, errcode.ErrParamFmt},
		{"超时越界", func(in *InterfaceInput) { in.Timeouts.ReadMS = maxTimeoutMS + 1 }, errcode.ErrParamFmt},
		{"请求头过多", func(in *InterfaceInput) {
			headers := make(map[string]string, maxHeaderCount+1)
			for i := 0; i <= maxHeaderCount; i++ {
				headers["X-H-"+string(rune('A'+i%26))+string(rune('a'+i/26))] = "v"
			}
			in.Headers = headers
		}, errcode.ErrParamFmt},
	}
	for _, c := range cases {
		in := validInterfaceInput()
		c.mut(&in)
		if _, err := svc.CreateInterface(ctx, 100, in); !errors.Is(err, c.want) {
			t.Errorf("%s: want %v, got %v", c.name, c.want, err)
		}
	}
	if len(repo.interfaces) != 0 {
		t.Fatalf("校验失败不应落库，接口数 = %d", len(repo.interfaces))
	}

	if _, err := svc.CreateInterface(ctx, 999, validInterfaceInput()); !errors.Is(err, errcode.ErrNotFound) {
		t.Fatalf("产品不存在 want 10301, got %v", err)
	}

	view, err := svc.CreateInterface(ctx, 100, validInterfaceInput())
	if err != nil {
		t.Fatalf("新建接口失败: %v", err)
	}
	if view.Status != domain.InterfaceStatusEnabled || view.Version != 1 {
		t.Fatalf("新建接口状态/版本异常: %+v", view)
	}
	if view.Method != domain.MethodPost || view.URL != "https://api.example.com/orders" {
		t.Fatalf("字段绑定异常: %+v", view)
	}
}

// TestInterfaceAuthHeaderEncrypted DES-I-001 / INV-7 / TEST-I-001：
// Authorization 等认证头以 AES-256-GCM 密文存 headers_enc，普通头留 headers；
// 视图只暴露认证头名称，任何响应结构都不包含明文值。
func TestInterfaceAuthHeaderEncrypted(t *testing.T) {
	svc, repo := newTestService()
	addProduct(repo, 100, "网关服务", "gateway", domain.ProductStatusEnabled)
	ctx := context.Background()

	view, err := svc.CreateInterface(ctx, 100, validInterfaceInput())
	if err != nil {
		t.Fatalf("新建接口失败: %v", err)
	}

	stored := repo.interfaces[view.ID]
	if len(stored.HeadersEnc) == 0 {
		t.Fatal("认证头必须落 headers_enc 密文")
	}
	if bytes.Contains(stored.HeadersEnc, []byte("super-secret-token")) {
		t.Fatal("headers_enc 中不得出现明文认证头（DES-I-001）")
	}
	if _, ok := stored.Headers["Authorization"]; ok {
		t.Fatal("认证头不得留在普通 headers 列")
	}
	if stored.Headers["Content-Type"] != "application/json" {
		t.Fatalf("普通头应留在 headers 列: %v", stored.Headers)
	}

	// 密文可解密回原值（供后续 k6 引擎使用），证明加密而非丢弃。
	plain, err := crypto.DecryptGCM(stored.HeadersEnc, testHeaderKey())
	if err != nil {
		t.Fatalf("解密认证头失败: %v", err)
	}
	var decrypted map[string]string
	if err := json.Unmarshal(plain, &decrypted); err != nil {
		t.Fatalf("解析认证头失败: %v", err)
	}
	if decrypted["Authorization"] != "Bearer super-secret-token" {
		t.Fatalf("认证头密文内容异常: %v", decrypted)
	}

	// 视图脱敏：只给名称，不含明文值。
	if len(view.AuthHeaders) != 1 || view.AuthHeaders[0] != "Authorization" {
		t.Fatalf("视图应仅返回认证头名称: %v", view.AuthHeaders)
	}
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("序列化视图失败: %v", err)
	}
	if strings.Contains(string(raw), "super-secret-token") {
		t.Fatalf("接口视图泄露明文认证头: %s", raw)
	}

	// 列表同样脱敏。
	page, err := svc.ListInterfaces(ctx, ListInterfacesQuery{ProductID: 100})
	if err != nil {
		t.Fatalf("接口列表失败: %v", err)
	}
	raw, _ = json.Marshal(page)
	if strings.Contains(string(raw), "super-secret-token") {
		t.Fatalf("接口列表泄露明文认证头: %s", raw)
	}
	if len(page.List) != 1 || len(page.List[0].AuthHeaders) != 1 {
		t.Fatalf("列表项认证头名称异常: %+v", page.List)
	}
}

// TestUpdateInterfaceKeepsEncryptedHeader 认证头留空表示沿用旧值；缺省表示删除。
func TestUpdateInterfaceKeepsEncryptedHeader(t *testing.T) {
	svc, repo := newTestService()
	addProduct(repo, 100, "网关服务", "gateway", domain.ProductStatusEnabled)
	ctx := context.Background()

	created, err := svc.CreateInterface(ctx, 100, validInterfaceInput())
	if err != nil {
		t.Fatalf("新建接口失败: %v", err)
	}

	in := validInterfaceInput()
	in.Version = created.Version
	in.URL = "https://api.example.com/v2/orders"
	in.Headers = map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "", // 留空 → 沿用旧密文值
		"Token":         "tk-new",
	}
	updated, err := svc.UpdateInterface(ctx, created.ID, in)
	if err != nil {
		t.Fatalf("修改接口失败: %v", err)
	}
	if updated.URL != "https://api.example.com/v2/orders" || updated.Version != created.Version+1 {
		t.Fatalf("修改结果异常: %+v", updated)
	}
	if len(updated.AuthHeaders) != 2 {
		t.Fatalf("认证头名称应为 Authorization+Token: %v", updated.AuthHeaders)
	}

	plain, err := crypto.DecryptGCM(repo.interfaces[created.ID].HeadersEnc, testHeaderKey())
	if err != nil {
		t.Fatalf("解密认证头失败: %v", err)
	}
	var decrypted map[string]string
	if err := json.Unmarshal(plain, &decrypted); err != nil {
		t.Fatalf("解析认证头失败: %v", err)
	}
	if decrypted["Authorization"] != "Bearer super-secret-token" {
		t.Fatalf("留空应沿用旧认证头值: %v", decrypted)
	}
	if decrypted["Token"] != "tk-new" {
		t.Fatalf("新认证头应写入: %v", decrypted)
	}

	// 认证头名称缺省 → 全部删除（全量替换语义）。
	in.Headers = map[string]string{"Content-Type": "application/json"}
	in.Version = updated.Version
	afterRemove, err := svc.UpdateInterface(ctx, created.ID, in)
	if err != nil {
		t.Fatalf("再次修改接口失败: %v", err)
	}
	if len(afterRemove.AuthHeaders) != 0 {
		t.Fatalf("认证头应被清空: %v", afterRemove.AuthHeaders)
	}
	if len(repo.interfaces[created.ID].HeadersEnc) != 0 {
		t.Fatal("认证头清空后 headers_enc 应为空（NULL）")
	}

	// 旧版本号再次提交 → 10402。
	in.Version = created.Version
	if _, err := svc.UpdateInterface(ctx, created.ID, in); !errors.Is(err, errcode.ErrConcurrent) {
		t.Fatalf("过期版本 want 10402, got %v", err)
	}
	// 缺 version → 10002。
	in.Version = 0
	if _, err := svc.UpdateInterface(ctx, created.ID, in); !errors.Is(err, errcode.ErrParamMiss) {
		t.Fatalf("缺 version want 10002, got %v", err)
	}
}

// TestInterfaceStatusAndDelete 接口状态机 enabled ↔ disabled；删除为逻辑删除。
func TestInterfaceStatusAndDelete(t *testing.T) {
	svc, repo := newTestService()
	addProduct(repo, 100, "网关服务", "gateway", domain.ProductStatusEnabled)
	addInterface(repo, 200, 100, "健康检查", domain.InterfaceStatusEnabled, nil)
	ctx := context.Background()

	if err := svc.UpdateInterfaceStatus(ctx, 200, "paused"); !errors.Is(err, errcode.ErrParamFmt) {
		t.Fatalf("非法状态 want 10003, got %v", err)
	}
	if err := svc.UpdateInterfaceStatus(ctx, 200, domain.InterfaceStatusEnabled); !errors.Is(err, errcode.ErrState) {
		t.Fatalf("同状态 want 10401, got %v", err)
	}
	if err := svc.UpdateInterfaceStatus(ctx, 200, domain.InterfaceStatusDisabled); err != nil {
		t.Fatalf("停用接口失败: %v", err)
	}
	if err := svc.UpdateInterfaceStatus(ctx, 999, domain.InterfaceStatusDisabled); !errors.Is(err, errcode.ErrNotFound) {
		t.Fatalf("接口不存在 want 10301, got %v", err)
	}

	if err := svc.DeleteInterface(ctx, 200); err != nil {
		t.Fatalf("删除接口失败: %v", err)
	}
	if len(repo.deletedInterfaces) != 1 || repo.deletedInterfaces[0] != 200 {
		t.Fatalf("应为逻辑删除: %v", repo.deletedInterfaces)
	}
	if err := svc.DeleteInterface(ctx, 200); !errors.Is(err, errcode.ErrNotFound) {
		t.Fatalf("重复删除 want 10301, got %v", err)
	}
}
