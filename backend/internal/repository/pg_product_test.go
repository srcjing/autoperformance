package repository

import (
	"encoding/json"
	"strings"
	"testing"

	"auto_performance/internal/domain"
)

// TestProductFilterClauseGuards 产品列表 SQL 必须包含软删除过滤、关键字/版本/平台/状态占位参数。
func TestProductFilterClauseGuards(t *testing.T) {
	clause := productFilterClause
	for _, want := range []string{
		"p.deleted_at IS NULL",
		"p.name ILIKE $1",
		"p.code ILIKE $1",
		"p.product_version = $2",
		"p.platform = $3",
		"p.status = $4",
	} {
		if !strings.Contains(clause, want) {
			t.Errorf("产品过滤条件缺少 %q: %s", want, clause)
		}
	}
}

// TestInterfaceFilterClauseGuards 接口列表 SQL 必须限定产品、软删除与状态过滤（接口严格归属产品）。
func TestInterfaceFilterClauseGuards(t *testing.T) {
	clause := interfaceFilterClause
	for _, want := range []string{
		"i.deleted_at IS NULL",
		"i.product_id = $1",
		"i.name ILIKE $2",
		"i.url ILIKE $2",
		"i.status = $3",
	} {
		if !strings.Contains(clause, want) {
			t.Errorf("接口过滤条件缺少 %q: %s", want, clause)
		}
	}
}

// TestScanColumnsContainSensitiveHeaderColumn 接口查询列必须包含 headers_enc（认证头密文列），
// 且不得回读到明文认证头字段（DES-I-001）。
func TestScanColumnsContainSensitiveHeaderColumn(t *testing.T) {
	if !strings.Contains(interfaceColumns, "i.headers_enc") {
		t.Fatalf("接口查询列缺少 headers_enc: %s", interfaceColumns)
	}
	if !strings.Contains(productColumns, "p.product_version") {
		t.Fatalf("产品查询列缺少 product_version: %s", productColumns)
	}
	if !strings.Contains(thresholdColumns, "metric") || !strings.Contains(thresholdColumns, "operator") {
		t.Fatalf("阈值查询列异常: %s", thresholdColumns)
	}
}

// TestMarshalJSONB nil/空集合统一序列化为 {}，其余按 JSON 输出。
func TestMarshalJSONB(t *testing.T) {
	raw, err := marshalJSONB(nil)
	if err != nil {
		t.Fatalf("序列化 nil 失败: %v", err)
	}
	if raw != "{}" {
		t.Fatalf("nil 应序列化为 {}，got %s", raw)
	}

	raw, err = marshalJSONB(map[string]string{"Content-Type": "application/json"})
	if err != nil {
		t.Fatalf("序列化请求头失败: %v", err)
	}
	var decoded map[string]string
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if decoded["Content-Type"] != "application/json" {
		t.Fatalf("请求头序列化异常: %s", raw)
	}

	raw, err = marshalJSONB(domain.InterfaceTimeouts{ConnectMS: 1000})
	if err != nil {
		t.Fatalf("序列化超时配置失败: %v", err)
	}
	var timeouts domain.InterfaceTimeouts
	if err := json.Unmarshal([]byte(raw), &timeouts); err != nil {
		t.Fatalf("反序列化超时配置失败: %v", err)
	}
	if timeouts.ConnectMS != 1000 {
		t.Fatalf("超时配置序列化异常: %s", raw)
	}
}
