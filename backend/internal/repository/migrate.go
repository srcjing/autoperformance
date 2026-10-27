package repository

import (
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/stdlib"

	"auto_performance/internal/config"
	"auto_performance/migrations"
)

// MigrateUp 执行全部数据库迁移（golang-migrate，内嵌 SQL）。
// 供 `auto_performance migrate up` 子命令与升级脚本（upgrade.sh）调用。
// 迁移完成后立即关闭连接；已是最新时（ErrNoChange）视为成功。
func MigrateUp(cfg config.DatabaseConfig) error {
	pool, err := Open(cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()

	driver, err := pgxmigrate.WithInstance(db, &pgxmigrate.Config{})
	if err != nil {
		return fmt.Errorf("初始化迁移驱动失败: %w", err)
	}

	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("初始化迁移源失败: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", src, cfg.Name, driver)
	if err != nil {
		return fmt.Errorf("初始化迁移器失败: %w", err)
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("执行迁移失败: %w", err)
	}
	return nil
}

// MigrateDown 回滚一步迁移（供升级失败回滚使用）。
func MigrateDown(cfg config.DatabaseConfig) error {
	pool, err := Open(cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()

	driver, err := pgxmigrate.WithInstance(db, &pgxmigrate.Config{})
	if err != nil {
		return fmt.Errorf("初始化迁移驱动失败: %w", err)
	}

	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("初始化迁移源失败: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", src, cfg.Name, driver)
	if err != nil {
		return fmt.Errorf("初始化迁移器失败: %w", err)
	}

	if err := m.Steps(-1); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("执行回滚失败: %w", err)
	}
	return nil
}
