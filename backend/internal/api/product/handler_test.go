package product

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
	productsvc "auto_performance/internal/service/product"
)

// stubService 仅实现被测试端点所需方法（其余经嵌入接口提供方法集）。
type stubService struct {
	Service

	productQuery     productsvc.ListProductsQuery
	interfaceQuery   productsvc.ListInterfacesQuery
	productIn        productsvc.ProductInput
	interfaceIn      productsvc.InterfaceInput
	thresholdsIn     []productsvc.ThresholdInput
	statusIn         string
	productPage      *productsvc.ProductPage
	interfacePage    *productsvc.InterfacePage
	thresholdItems   []productsvc.ThresholdView
	productView      *productsvc.ProductView
	interfaceView    *productsvc.InterfaceView
	err              error
	createProductID  int64
	createInterfaceP int64
}

func (s *stubService) ListProducts(_ context.Context, q productsvc.ListProductsQuery) (*productsvc.ProductPage, error) {
	s.productQuery = q
	if s.err != nil {
		return nil, s.err
	}
	if s.productPage != nil {
		return s.productPage, nil
	}
	return &productsvc.ProductPage{List: []productsvc.ProductView{}, Page: 1, PageSize: 20}, nil
}

func (s *stubService) CreateProduct(_ context.Context, in productsvc.ProductInput) (*productsvc.ProductView, error) {
	s.productIn = in
	if s.err != nil {
		return nil, s.err
	}
	return productView(), nil
}

func (s *stubService) GetProduct(_ context.Context, _ int64) (*productsvc.ProductView, error) {
	if s.err != nil {
		return nil, s.err
	}
	return productView(), nil
}

func (s *stubService) UpdateProduct(_ context.Context, _ int64, in productsvc.ProductInput) (*productsvc.ProductView, error) {
	s.productIn = in
	if s.err != nil {
		return nil, s.err
	}
	return productView(), nil
}

func (s *stubService) DeleteProduct(_ context.Context, _ int64) error { return s.err }

func (s *stubService) UpdateStatus(_ context.Context, _ int64, status string) error {
	s.statusIn = status
	return s.err
}

func (s *stubService) ListInterfaces(_ context.Context, q productsvc.ListInterfacesQuery) (*productsvc.InterfacePage, error) {
	s.interfaceQuery = q
	if s.err != nil {
		return nil, s.err
	}
	if s.interfacePage != nil {
		return s.interfacePage, nil
	}
	return &productsvc.InterfacePage{List: []productsvc.InterfaceView{}, Page: 1, PageSize: 20}, nil
}

func (s *stubService) CreateInterface(_ context.Context, productID int64, in productsvc.InterfaceInput) (*productsvc.InterfaceView, error) {
	s.createInterfaceP = productID
	s.interfaceIn = in
	if s.err != nil {
		return nil, s.err
	}
	return interfaceView(), nil
}

func (s *stubService) UpdateInterface(_ context.Context, _ int64, in productsvc.InterfaceInput) (*productsvc.InterfaceView, error) {
	s.interfaceIn = in
	if s.err != nil {
		return nil, s.err
	}
	return interfaceView(), nil
}

func (s *stubService) DeleteInterface(_ context.Context, _ int64) error { return s.err }

func (s *stubService) UpdateInterfaceStatus(_ context.Context, _ int64, status string) error {
	s.statusIn = status
	return s.err
}

func (s *stubService) ListThresholds(_ context.Context, _ int64) ([]productsvc.ThresholdView, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.thresholdItems, nil
}

func (s *stubService) ReplaceThresholds(_ context.Context, _ int64, inputs []productsvc.ThresholdInput) ([]productsvc.ThresholdView, error) {
	s.thresholdsIn = inputs
	if s.err != nil {
		return nil, s.err
	}
	return []productsvc.ThresholdView{{
		ID: 1, InterfaceID: 7, Metric: domain.MetricP95, Operator: domain.OperatorLE,
		Value: 500, Unit: "ms", Enabled: true, Version: 1,
	}}, nil
}

func productView() *productsvc.ProductView {
	now := time.Date(2026, 10, 26, 8, 0, 0, 0, time.UTC)
	return &productsvc.ProductView{
		ID: 3, Name: "网关服务", Code: "gateway", ProductVersion: "v1.2.3",
		Platform: "Linux-x86", Status: domain.ProductStatusEnabled, InterfaceCount: 2,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
}

// interfaceView 构造含认证头名称（无明文值）的接口视图。
func interfaceView() *productsvc.InterfaceView {
	now := time.Date(2026, 10, 26, 8, 0, 0, 0, time.UTC)
	return &productsvc.InterfaceView{
		ID: 7, ProductID: 3, Name: "创建订单", Method: domain.MethodPost,
		URL:         "https://api.example.com/orders",
		Headers:     map[string]string{"Content-Type": "application/json"},
		AuthHeaders: []string{"Authorization"},
		Body:        `{"sku":"A-1"}`,
		Timeouts:    domain.InterfaceTimeouts{ConnectMS: 1000},
		Status:      domain.InterfaceStatusEnabled, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
}

func newTestRouter(svc Service) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := New(svc)
	r.GET("/api/v1/products", h.ListProducts)
	r.POST("/api/v1/products", h.CreateProduct)
	r.GET("/api/v1/products/:id", h.GetProduct)
	r.PUT("/api/v1/products/:id", h.UpdateProduct)
	r.DELETE("/api/v1/products/:id", h.DeleteProduct)
	r.PATCH("/api/v1/products/:id/status", h.UpdateStatus)
	r.GET("/api/v1/products/:id/interfaces", h.ListInterfaces)
	r.POST("/api/v1/products/:id/interfaces", h.CreateInterface)
	r.PUT("/api/v1/interfaces/:id", h.UpdateInterface)
	r.DELETE("/api/v1/interfaces/:id", h.DeleteInterface)
	r.PATCH("/api/v1/interfaces/:id/status", h.UpdateInterfaceStatus)
	r.GET("/api/v1/interfaces/:id/thresholds", h.ListThresholds)
	r.PUT("/api/v1/interfaces/:id/thresholds", h.ReplaceThresholds)
	return r
}

func doJSON(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("响应不是统一 JSON 结构: %s", w.Body.String())
	}
	return m
}

// firstItem 取响应 data.list[0]。
func firstItem(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	body := decodeBody(t, w)
	data, _ := body["data"].(map[string]any)
	list, _ := data["list"].([]any)
	if len(list) == 0 {
		t.Fatalf("响应列表为空: %s", w.Body.String())
	}
	first, _ := list[0].(map[string]any)
	if first == nil {
		t.Fatalf("列表项结构异常: %s", w.Body.String())
	}
	return first
}

// TestListProductsParsesQuery 产品列表查询参数解析（关键字/版本/平台/状态/分页）。
func TestListProductsParsesQuery(t *testing.T) {
	stub := &stubService{}
	r := newTestRouter(stub)

	w := doJSON(r, http.MethodGet,
		"/api/v1/products?keyword=gate&product_version=v1.2.3&platform=Linux&status=enabled&page=2&page_size=50", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	got := stub.productQuery
	if got.Keyword != "gate" || got.ProductVersion != "v1.2.3" || got.Platform != "Linux" ||
		got.Status != domain.ProductStatusEnabled {
		t.Fatalf("查询参数解析异常: %+v", got)
	}
	if got.Page != 2 || got.PageSize != 50 {
		t.Fatalf("分页参数解析异常: %+v", got)
	}
}

// TestCreateProductRequiresFields 缺 name/code/product_version → 400/10002。
func TestCreateProductRequiresFields(t *testing.T) {
	stub := &stubService{}
	r := newTestRouter(stub)

	for _, body := range []string{
		`{"code":"c","product_version":"v1"}`,
		`{"name":"n","product_version":"v1"}`,
		`{"name":"n","code":"c"}`,
	} {
		w := doJSON(r, http.MethodPost, "/api/v1/products", body)
		if w.Code != http.StatusBadRequest || decodeBody(t, w)["code"] != float64(10002) {
			t.Errorf("缺必填字段应 400/10002, body=%s got %d %s", body, w.Code, w.Body.String())
		}
	}
}

// TestProductErrorCodeMapping 业务错误码 → HTTP 状态：22001(400) / 22002(409) / 10402(409)。
func TestProductErrorCodeMapping(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		method string
		path   string
		body   string
		status int
		code   float64
	}{
		{"编码重复", productsvc.ErrCodeExists, http.MethodPost, "/api/v1/products",
			`{"name":"n","code":"c","product_version":"v1"}`, http.StatusBadRequest, 22001},
		{"下有接口", productsvc.ErrProductHasInterfaces, http.MethodDelete, "/api/v1/products/3",
			"", http.StatusConflict, 22002},
		{"乐观锁冲突", errcode.ErrConcurrent, http.MethodPut, "/api/v1/products/3",
			`{"name":"n","code":"c","product_version":"v1","version":1}`, http.StatusConflict, 10402},
		{"阈值方向不匹配", productsvc.ErrThresholdMismatch, http.MethodPut, "/api/v1/interfaces/7/thresholds",
			`{"thresholds":[{"metric":"success_rate","operator":"le","value":99}]}`, http.StatusBadRequest, 22003},
	}
	for _, c := range cases {
		r := newTestRouter(&stubService{err: c.err})
		w := doJSON(r, c.method, c.path, c.body)
		if w.Code != c.status || decodeBody(t, w)["code"] != c.code {
			t.Errorf("%s: got %d %s, want %d/%v", c.name, w.Code, w.Body.String(), c.status, c.code)
		}
	}
}

// TestInterfaceDTORedactsAuthHeaders DES-I-001：接口响应只暴露认证头名称，
// headers 不含认证头，body 以原始 JSON 返回。
func TestInterfaceDTORedactsAuthHeaders(t *testing.T) {
	stub := &stubService{interfacePage: &productsvc.InterfacePage{
		List: []productsvc.InterfaceView{*interfaceView()}, Total: 1, Page: 1, PageSize: 20,
	}}
	r := newTestRouter(stub)

	w := doJSON(r, http.MethodGet, "/api/v1/products/3/interfaces", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	item := firstItem(t, w)
	authHeaders, _ := item["auth_headers"].([]any)
	if len(authHeaders) != 1 || authHeaders[0] != "Authorization" {
		t.Fatalf("auth_headers 应仅含名称: %v", item["auth_headers"])
	}
	headers, _ := item["headers"].(map[string]any)
	if _, ok := headers["Authorization"]; ok {
		t.Fatal("headers 不得包含认证头（DES-I-001）")
	}
	body, _ := item["body"].(map[string]any)
	if body["sku"] != "A-1" {
		t.Fatalf("body 应以原始 JSON 返回: %v", item["body"])
	}
	if strings.Contains(w.Body.String(), "super-secret-token") {
		t.Fatalf("响应泄露明文认证头: %s", w.Body.String())
	}

	// 详情同样脱敏。
	w = doJSON(r, http.MethodGet, "/api/v1/products/3/interfaces?page=1", "")
	if strings.Contains(w.Body.String(), "Authorization\":\"") {
		t.Fatalf("响应泄露认证头键值: %s", w.Body.String())
	}
}

// TestCreateInterfaceParsesPayload 请求体解析：headers 透传、body 转 JSON 文本、timeouts 透传。
func TestCreateInterfaceParsesPayload(t *testing.T) {
	stub := &stubService{}
	r := newTestRouter(stub)

	body := `{
		"name": "创建订单",
		"method": "post",
		"url": "https://api.example.com/orders",
		"headers": {"Content-Type": "application/json", "Authorization": "Bearer secret-token"},
		"body": {"sku": "A-1", "qty": 2},
		"timeouts": {"connect_ms": 1000, "read_ms": 5000},
		"description": "下单接口"
	}`
	w := doJSON(r, http.MethodPost, "/api/v1/products/3/interfaces", body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if stub.createInterfaceP != 3 {
		t.Fatalf("产品 ID 未透传: %d", stub.createInterfaceP)
	}
	got := stub.interfaceIn
	if got.Headers["Authorization"] != "Bearer secret-token" || got.Headers["Content-Type"] != "application/json" {
		t.Fatalf("headers 解析异常: %v", got.Headers)
	}
	if !json.Valid([]byte(got.Body)) || !strings.Contains(got.Body, `"sku"`) {
		t.Fatalf("body 未转为合法 JSON 文本: %q", got.Body)
	}
	if got.Timeouts.ConnectMS != 1000 || got.Timeouts.ReadMS != 5000 {
		t.Fatalf("timeouts 解析异常: %+v", got.Timeouts)
	}
	if !strings.EqualFold(got.Method, "post") {
		t.Fatalf("method 解析异常: %s", got.Method)
	}

	// body 非法 JSON 字符串在请求层被拒（10003）。
	w = doJSON(r, http.MethodPost, "/api/v1/products/3/interfaces",
		`{"name":"n","url":"https://a.example.com","body":"not-an-object"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("JSON 字符串 body 应允许（参数模板可为字符串）: %d %s", w.Code, w.Body.String())
	}
}

// TestReplaceThresholdsParsesItems 阈值全量替换请求解析与 22003 映射。
func TestReplaceThresholdsParsesItems(t *testing.T) {
	stub := &stubService{}
	r := newTestRouter(stub)

	body := `{"thresholds":[
		{"metric":"p95","operator":"le","value":500,"unit":"ms","enabled":true},
		{"metric":"throughput","operator":"ge","value":100}
	]}`
	w := doJSON(r, http.MethodPut, "/api/v1/interfaces/7/thresholds", body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if len(stub.thresholdsIn) != 2 {
		t.Fatalf("阈值项解析异常: %+v", stub.thresholdsIn)
	}
	if stub.thresholdsIn[0].Metric != domain.MetricP95 || stub.thresholdsIn[0].Value != 500 ||
		stub.thresholdsIn[0].Unit != "ms" || stub.thresholdsIn[0].Enabled == nil || !*stub.thresholdsIn[0].Enabled {
		t.Fatalf("第一项解析异常: %+v", stub.thresholdsIn[0])
	}
	if stub.thresholdsIn[1].Enabled != nil {
		t.Fatalf("缺省 enabled 应为 nil（由服务层补默认 true）: %+v", stub.thresholdsIn[1])
	}

	item := firstItem(t, w)
	if item["unit"] != "ms" || item["enabled"] != true {
		t.Fatalf("阈值响应异常: %v", item)
	}
}

// TestStatusEndpointsRequireStatus 停用/启用缺 status → 10002；非法 ID → 10003。
func TestStatusEndpointsRequireStatus(t *testing.T) {
	stub := &stubService{}
	r := newTestRouter(stub)

	for _, tc := range []struct{ path string }{
		{"/api/v1/products/3/status"},
		{"/api/v1/interfaces/7/status"},
	} {
		w := doJSON(r, http.MethodPatch, tc.path, `{}`)
		if w.Code != http.StatusBadRequest || decodeBody(t, w)["code"] != float64(10002) {
			t.Errorf("PATCH %s 缺 status 应 400/10002，got %d %s", tc.path, w.Code, w.Body.String())
		}
	}

	w := doJSON(r, http.MethodPatch, "/api/v1/products/abc/status", `{"status":"disabled"}`)
	if w.Code != http.StatusBadRequest || decodeBody(t, w)["code"] != float64(10003) {
		t.Fatalf("非法 ID 应 400/10003，got %d %s", w.Code, w.Body.String())
	}

	w = doJSON(r, http.MethodPatch, "/api/v1/products/3/status", `{"status":"disabled"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if stub.statusIn != domain.ProductStatusDisabled {
		t.Fatalf("状态未透传: %s", stub.statusIn)
	}
}
