package repository

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"auto_performance/internal/errcode"
)

// TestMarshalHosts 主机列表序列化为 JSONB 文本；nil → 空数组（DB NOT NULL 语义友好）。
func TestMarshalHosts(t *testing.T) {
	raw, err := marshalHosts(nil)
	if err != nil {
		t.Fatalf("序列化 nil 主机失败: %v", err)
	}
	if raw != "[]" {
		t.Fatalf("nil 主机应序列化为 []，got %s", raw)
	}

	raw, err = marshalHosts([]string{"10.0.0.1", "db.internal"})
	if err != nil {
		t.Fatalf("序列化主机失败: %v", err)
	}
	var decoded []string
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if len(decoded) != 2 || decoded[1] != "db.internal" {
		t.Fatalf("主机序列化异常: %s", raw)
	}
}

// TestNullableID 0 视为未绑定（DB NULL）；正值原样传入。
func TestNullableID(t *testing.T) {
	if v := nullableID(0); v != nil {
		t.Fatalf("0 应映射为 NULL，got %v", v)
	}
	if v := nullableID(-1); v != nil {
		t.Fatalf("负数应映射为 NULL，got %v", v)
	}
	if v, ok := nullableID(9).(int64); !ok || v != 9 {
		t.Fatalf("正值应原样传递，got %v", nullableID(9))
	}
}

// TestEnvFilterClauseGuards env 列表过滤 SQL 必须包含软删除过滤、标签交集与四个占位参数。
func TestEnvFilterClauseGuards(t *testing.T) {
	clause := envFilterClause
	for _, want := range []string{
		"e.deleted_at IS NULL",
		"e.name ILIKE $1",
		"e.category_id = $2",
		"e.status = $3",
		"ANY($4::bigint[])",
		"cardinality($4::bigint[])",
	} {
		if !strings.Contains(clause, want) {
			t.Errorf("过滤条件缺少 %q: %s", want, clause)
		}
	}
}

// TestIsUniqueViolation / TestIsForeignKeyViolation 约束冲突识别（错误码映射到业务语义）。
func TestIsUniqueViolation(t *testing.T) {
	if !isUniqueViolation(&pgconn.PgError{Code: "23505"}) {
		t.Fatal("23505 应识别为唯一约束冲突")
	}
	if isUniqueViolation(&pgconn.PgError{Code: "23503"}) {
		t.Fatal("23503 不应识别为唯一约束冲突")
	}
	if isUniqueViolation(errcode.ErrNotFound) {
		t.Fatal("非 PG 错误不应识别为唯一约束冲突")
	}
}

func TestIsForeignKeyViolation(t *testing.T) {
	if !isForeignKeyViolation(&pgconn.PgError{Code: "23503"}) {
		t.Fatal("23503 应识别为外键约束冲突")
	}
	if isForeignKeyViolation(&pgconn.PgError{Code: "23505"}) {
		t.Fatal("23505 不应识别为外键约束冲突")
	}
}
