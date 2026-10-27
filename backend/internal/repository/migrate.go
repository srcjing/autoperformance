package repository

import (
	"errors"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // 注册 pgx5:// 驱动
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"auto_performance/internal/config"
	"auto_performance/migrations"
)

// MigrateUp 执行全部数据库迁移（golang-migrate，内嵌 SQL）。
// 供 `auto_performance migrate up` 子命令与升级脚本（upgrade.sh）调用。
// 迁移完成后立即关闭连接；已是最新时（ErrNoChange）视为成功。
//
// 实现说明：通过 DSN 让 migrate 自行管理连接（pgx5:// 驱动），
// 不与业务 pgxpool 混用，避免 migrate 内部 goroutine 未及时回收连接
// 导致 `pool.Close()` 的 WaitGroup 死锁。
func MigrateUp(cfg config.DatabaseConfig) error {
	m, err := newMigrator(cfg)
	if err != nil {
		return err
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("执行迁移失败: %w", err)
	}
	return nil
}

// MigrateDown 回滚一步迁移（供升级失败回滚使用）。
func MigrateDown(cfg config.DatabaseConfig) error {
	m, err := newMigrator(cfg)
	if err != nil {
		return err
	}
	defer m.Close()

	if err := m.Steps(-1); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("执行回滚失败: %w", err)
	}
	return nil
}

// newMigrator 构造迁移器：iofs 内嵌迁移源 + pgx5 DSN 数据库驱动。
func newMigrator(cfg config.DatabaseConfig) (*migrate.Migrate, error) {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("初始化迁移源失败: %w", err)
	}

	// buildDSN 产出 postgres:// 连接串；migrate 的 pgx/v5 驱动注册的 scheme 为 pgx5。
	dbURL := strings.Replace(buildDSN(cfg), "postgres://", "pgx5://", 1)

	m, err := migrate.NewWithSourceInstance("iofs", src, dbURL)
	if err != nil {
		return nil, fmt.Errorf("初始化迁移器失败: %w", err)
	}
	return m, nil
}
