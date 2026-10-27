package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"auto_performance/internal/errcode"
)

// fakeExecer 内存假执行器：模拟 pgx Exec，返回指定 RowsAffected。
type fakeExecer struct {
	rowsAffected int64
	lastSQL      string
	lastArgs     []any
}

func (f *fakeExecer) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.lastSQL = sql
	f.lastArgs = args
	return pgconn.NewCommandTag(fmt.Sprintf("UPDATE %d", f.rowsAffected)), nil
}

func TestOptimisticUpdateConflict(t *testing.T) {
	ctx := context.Background()

	t.Run("rows_affected_zero_returns_conflict", func(t *testing.T) {
		e := &fakeExecer{rowsAffected: 0}
		err := optimisticUpdate(ctx, e, "environment", 1, 3, map[string]any{"name": "x"})
		if !errors.Is(err, errcode.ErrConcurrent) {
			t.Fatalf("want ErrConcurrent, got %v", err)
		}
	})

	t.Run("rows_affected_one_succeeds", func(t *testing.T) {
		e := &fakeExecer{rowsAffected: 1}
		err := optimisticUpdate(ctx, e, "environment", 1, 3, map[string]any{"name": "x", "description": "y"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// 校验乐观锁关键条件已生成
		sql := strings.ToUpper(e.lastSQL)
		if !strings.Contains(sql, "WHERE ID = $1 AND VERSION = $2") {
			t.Errorf("SQL 缺少乐观锁条件: %s", e.lastSQL)
		}
		if !strings.Contains(sql, "DELETED_AT IS NULL") {
			t.Errorf("SQL 缺少软删除过滤: %s", e.lastSQL)
		}
		if !strings.Contains(sql, "VERSION = VERSION + 1") {
			t.Errorf("SQL 缺少 version 自增: %s", e.lastSQL)
		}
		// 参数顺序：id, version, 然后 set 值（按列名排序）
		if len(e.lastArgs) != 4 {
			t.Fatalf("参数数量 = %d, want 4 (id, version, 2 set values)", len(e.lastArgs))
		}
		if e.lastArgs[0] != int64(1) || e.lastArgs[1] != int64(3) {
			t.Errorf("前两个参数应为 id/version: %v", e.lastArgs[:2])
		}
	})
}

func TestApplySoftDelete(t *testing.T) {
	e := &fakeExecer{rowsAffected: 1}
	if err := applySoftDelete(context.Background(), e, "product", 42); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(strings.ToUpper(e.lastSQL), "SET DELETED_AT = NOW()") {
		t.Errorf("软删除 SQL 异常: %s", e.lastSQL)
	}
	if e.lastArgs[0] != int64(42) {
		t.Errorf("软删除参数异常: %v", e.lastArgs)
	}
}
