package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
)

// 产品管理相关查询列（顺序与 scan* 一致）。
const (
	productColumns = `p.id, p.name, p.code, COALESCE(p.product_version, ''), COALESCE(p.platform, ''),
		COALESCE(p.description, ''), p.status, p.version, p.created_at, p.updated_at`

	interfaceColumns = `i.id, i.product_id, i.name, COALESCE(i.method, ''), COALESCE(i.url, ''),
		COALESCE(i.headers, '{}'::jsonb), COALESCE(i.headers_enc, ''::bytea), COALESCE(i.body::text, ''),
		COALESCE(i.timeouts, '{}'::jsonb), COALESCE(i.description, ''), i.status, i.version,
		i.created_at, i.updated_at`

	thresholdColumns = `id, interface_id, metric, operator, value, COALESCE(unit, ''), enabled,
		version, created_at, updated_at`
)

// scanProduct 扫描一行产品（columns 与 productColumns 一致，可附带接口数）。
func scanProduct(row pgx.Row, withCount bool) (*domain.Product, error) {
	var p domain.Product
	dest := []any{
		&p.ID, &p.Name, &p.Code, &p.ProductVersion, &p.Platform,
		&p.Description, &p.Status, &p.Version, &p.CreatedAt, &p.UpdatedAt,
	}
	if withCount {
		dest = append(dest, &p.InterfaceCount)
	}
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	return &p, nil
}

// scanInterface 扫描一行接口（headers/timeouts 由 JSONB 解码）。
func scanInterface(row pgx.Row) (*domain.Interface, error) {
	var api domain.Interface
	var headers, timeouts []byte
	err := row.Scan(
		&api.ID, &api.ProductID, &api.Name, &api.Method, &api.URL, &headers, &api.HeadersEnc,
		&api.Body, &timeouts, &api.Description, &api.Status, &api.Version,
		&api.CreatedAt, &api.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if len(headers) > 0 {
		if err := json.Unmarshal(headers, &api.Headers); err != nil {
			return nil, fmt.Errorf("解析接口请求头失败: %w", err)
		}
	}
	if api.Headers == nil {
		api.Headers = map[string]string{}
	}
	if len(timeouts) > 0 {
		if err := json.Unmarshal(timeouts, &api.Timeouts); err != nil {
			return nil, fmt.Errorf("解析接口超时配置失败: %w", err)
		}
	}
	return &api, nil
}

// scanThreshold 扫描一行阈值。
func scanThreshold(row pgx.Row) (*domain.Threshold, error) {
	var t domain.Threshold
	err := row.Scan(&t.ID, &t.InterfaceID, &t.Metric, &t.Operator, &t.Value, &t.Unit,
		&t.Enabled, &t.Version, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// marshalJSONB 序列化为 JSONB 文本；nil/空集合统一输出 "{}"。
func marshalJSONB(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("序列化 JSON 字段失败: %w", err)
	}
	if string(raw) == "null" {
		return "{}", nil
	}
	return string(raw), nil
}

// ===================== 产品 =====================

// productFilterClause 产品列表公共过滤条件（$1 关键字、$2 版本、$3 平台、$4 状态）。
const productFilterClause = `
	WHERE p.deleted_at IS NULL
	  AND ($1 = '' OR p.name ILIKE $1 OR p.code ILIKE $1)
	  AND ($2 = '' OR p.product_version = $2)
	  AND ($3 = '' OR p.platform = $3)
	  AND ($4 = '' OR p.status = $4)`

// ListProducts 产品列表（关键字/版本/平台/状态 + 分页，含未删除接口数）。
func (r *PGRepository) ListProducts(ctx context.Context, q domain.ProductQuery) ([]domain.Product, int64, error) {
	pattern := ""
	if q.Keyword != "" {
		pattern = "%" + q.Keyword + "%"
	}

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM product p `+productFilterClause,
		pattern, q.ProductVersion, q.Platform, q.Status).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("统计产品失败: %w", err)
	}

	offset := (q.Page - 1) * q.PageSize
	if offset < 0 {
		offset = 0
	}
	query := `
		SELECT ` + productColumns + `, count(i.id)
		FROM product p
		LEFT JOIN interface i ON i.product_id = p.id AND i.deleted_at IS NULL
		` + productFilterClause + `
		GROUP BY p.id
		ORDER BY p.id
		LIMIT $5 OFFSET $6`
	rows, err := r.pool.Query(ctx, query, pattern, q.ProductVersion, q.Platform, q.Status, q.PageSize, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("查询产品列表失败: %w", err)
	}
	defer rows.Close()

	out := make([]domain.Product, 0, q.PageSize)
	for rows.Next() {
		p, err := scanProduct(rows, true)
		if err != nil {
			return nil, 0, fmt.Errorf("扫描产品失败: %w", err)
		}
		out = append(out, *p)
	}
	return out, total, rows.Err()
}

// GetProduct 产品详情（含接口数；不存在 → 10301）。
func (r *PGRepository) GetProduct(ctx context.Context, productID int64) (*domain.Product, error) {
	const q = `
		SELECT ` + productColumns + `, count(i.id)
		FROM product p
		LEFT JOIN interface i ON i.product_id = p.id AND i.deleted_at IS NULL
		WHERE p.id = $1 AND p.deleted_at IS NULL
		GROUP BY p.id`
	p, err := scanProduct(r.pool.QueryRow(ctx, q, productID), true)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errcode.ErrNotFound
		}
		return nil, fmt.Errorf("查询产品失败: %w", err)
	}
	return p, nil
}

// CreateProduct 新建产品（编码唯一冲突 → 10302，由服务层映射 22001）。
func (r *PGRepository) CreateProduct(ctx context.Context, p *domain.Product) error {
	const q = `
		INSERT INTO product (name, code, product_version, platform, description, status)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, version, created_at, updated_at`
	err := r.pool.QueryRow(ctx, q,
		p.Name, p.Code, p.ProductVersion, p.Platform, p.Description, p.Status,
	).Scan(&p.ID, &p.Version, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return errcode.ErrConflict
		}
		return fmt.Errorf("创建产品失败: %w", err)
	}
	return nil
}

// UpdateProduct 全量修改产品（version 乐观锁：冲突 → 10402，编码重复 → 10302）。
func (r *PGRepository) UpdateProduct(ctx context.Context, p *domain.Product, version int64) error {
	sets := map[string]any{
		"name":            p.Name,
		"code":            p.Code,
		"product_version": p.ProductVersion,
		"platform":        p.Platform,
		"description":     p.Description,
	}
	if err := optimisticUpdate(ctx, r.pool, "product", p.ID, version, sets); err != nil {
		if isUniqueViolation(err) {
			return errcode.ErrConflict
		}
		return err
	}
	return nil
}

// UpdateProductStatus 停用/启用产品（不存在 → 10301）。
func (r *PGRepository) UpdateProductStatus(ctx context.Context, productID int64, status string) error {
	const q = `UPDATE product SET status = $2, version = version + 1, updated_at = now()
	           WHERE id = $1 AND deleted_at IS NULL`
	tag, err := r.pool.Exec(ctx, q, productID, status)
	if err != nil {
		return fmt.Errorf("修改产品状态失败: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errcode.ErrNotFound
	}
	return nil
}

// SoftDeleteProduct 逻辑删除产品；存在未删除接口时拒绝（→ errcode.ErrReferenced）。
func (r *PGRepository) SoftDeleteProduct(ctx context.Context, productID int64) error {
	const q = `UPDATE product SET deleted_at = now()
	           WHERE id = $1 AND deleted_at IS NULL
	             AND NOT EXISTS (
	               SELECT 1 FROM interface i WHERE i.product_id = $1 AND i.deleted_at IS NULL
	             )`
	tag, err := r.pool.Exec(ctx, q, productID)
	if err != nil {
		return fmt.Errorf("删除产品失败: %w", err)
	}
	if tag.RowsAffected() > 0 {
		return nil
	}

	// 未命中：区分「不存在」与「下存在接口」（并发兜底，业务提示由服务层统一给出）。
	var refs int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM interface i
	                               WHERE i.product_id = $1 AND i.deleted_at IS NULL`, productID).Scan(&refs); err != nil {
		return fmt.Errorf("校验产品引用失败: %w", err)
	}
	if refs > 0 {
		return errcode.ErrReferenced
	}
	return errcode.ErrNotFound
}

// CountInterfacesByProduct 统计产品下未删除接口数。
func (r *PGRepository) CountInterfacesByProduct(ctx context.Context, productID int64) (int64, error) {
	const q = `SELECT count(*) FROM interface WHERE product_id = $1 AND deleted_at IS NULL`
	var n int64
	if err := r.pool.QueryRow(ctx, q, productID).Scan(&n); err != nil {
		return 0, fmt.Errorf("统计产品接口失败: %w", err)
	}
	return n, nil
}

// ===================== 接口 =====================

// interfaceFilterClause 接口列表公共过滤条件（$1 产品、$2 关键字、$3 状态）。
const interfaceFilterClause = `
	WHERE i.deleted_at IS NULL
	  AND i.product_id = $1
	  AND ($2 = '' OR i.name ILIKE $2 OR i.url ILIKE $2)
	  AND ($3 = '' OR i.status = $3)`

// ListInterfaces 产品下接口列表（关键字/状态 + 分页）。
func (r *PGRepository) ListInterfaces(ctx context.Context, q domain.InterfaceQuery) ([]domain.Interface, int64, error) {
	pattern := ""
	if q.Keyword != "" {
		pattern = "%" + q.Keyword + "%"
	}

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM interface i `+interfaceFilterClause,
		q.ProductID, pattern, q.Status).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("统计接口失败: %w", err)
	}

	offset := (q.Page - 1) * q.PageSize
	if offset < 0 {
		offset = 0
	}
	query := `SELECT ` + interfaceColumns + ` FROM interface i ` + interfaceFilterClause +
		` ORDER BY i.id LIMIT $4 OFFSET $5`
	rows, err := r.pool.Query(ctx, query, q.ProductID, pattern, q.Status, q.PageSize, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("查询接口列表失败: %w", err)
	}
	defer rows.Close()

	out := make([]domain.Interface, 0, q.PageSize)
	for rows.Next() {
		api, err := scanInterface(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("扫描接口失败: %w", err)
		}
		out = append(out, *api)
	}
	return out, total, rows.Err()
}

// GetInterface 接口详情（不存在 → 10301）。
func (r *PGRepository) GetInterface(ctx context.Context, interfaceID int64) (*domain.Interface, error) {
	const q = `SELECT ` + interfaceColumns + ` FROM interface i WHERE i.id = $1 AND i.deleted_at IS NULL`
	api, err := scanInterface(r.pool.QueryRow(ctx, q, interfaceID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errcode.ErrNotFound
		}
		return nil, fmt.Errorf("查询接口失败: %w", err)
	}
	return api, nil
}

// CreateInterface 新建接口（headers_enc 由服务层加密后传入，DES-I-001）。
func (r *PGRepository) CreateInterface(ctx context.Context, api *domain.Interface) error {
	headers, err := marshalJSONB(api.Headers)
	if err != nil {
		return err
	}
	timeouts, err := marshalJSONB(api.Timeouts)
	if err != nil {
		return err
	}
	const q = `
		INSERT INTO interface (product_id, name, method, url, headers, headers_enc, body, timeouts,
		                       description, status, enabled)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6, CASE WHEN $7 = '' THEN NULL ELSE $7::jsonb END,
		        $8::jsonb, $9, $10, $11)
		RETURNING id, version, created_at, updated_at`
	// enabled 为历史兼容列，与 status 保持同步（唯一事实源为 status）。
	err = r.pool.QueryRow(ctx, q,
		api.ProductID, api.Name, api.Method, api.URL, headers, api.HeadersEnc, api.Body,
		timeouts, api.Description, api.Status, api.Status == domain.InterfaceStatusEnabled,
	).Scan(&api.ID, &api.Version, &api.CreatedAt, &api.UpdatedAt)
	if err != nil {
		if isForeignKeyViolation(err) {
			return errcode.ErrNotFound
		}
		return fmt.Errorf("创建接口失败: %w", err)
	}
	return nil
}

// UpdateInterface 全量修改接口（version 乐观锁：冲突 → 10402）。
func (r *PGRepository) UpdateInterface(ctx context.Context, api *domain.Interface, version int64) error {
	headers, err := marshalJSONB(api.Headers)
	if err != nil {
		return err
	}
	timeouts, err := marshalJSONB(api.Timeouts)
	if err != nil {
		return err
	}
	const q = `
		UPDATE interface
		SET name = $3, method = $4, url = $5, headers = $6::jsonb, headers_enc = $7,
		    body = CASE WHEN $8 = '' THEN NULL ELSE $8::jsonb END, timeouts = $9::jsonb,
		    description = $10, version = version + 1, updated_at = now()
		WHERE id = $1 AND version = $2 AND deleted_at IS NULL`
	tag, err := r.pool.Exec(ctx, q,
		api.ID, version, api.Name, api.Method, api.URL, headers, api.HeadersEnc, api.Body,
		timeouts, api.Description,
	)
	if err != nil {
		return fmt.Errorf("修改接口失败: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errcode.ErrConcurrent
	}
	return nil
}

// UpdateInterfaceStatus 停用/启用接口（不存在 → 10301）。
func (r *PGRepository) UpdateInterfaceStatus(ctx context.Context, interfaceID int64, status string) error {
	const q = `UPDATE interface SET status = $2, enabled = ($2 = 'enabled'),
	           version = version + 1, updated_at = now()
	           WHERE id = $1 AND deleted_at IS NULL`
	tag, err := r.pool.Exec(ctx, q, interfaceID, status)
	if err != nil {
		return fmt.Errorf("修改接口状态失败: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errcode.ErrNotFound
	}
	return nil
}

// SoftDeleteInterface 逻辑删除接口（阈值保留，历史快照可追溯）。
func (r *PGRepository) SoftDeleteInterface(ctx context.Context, interfaceID int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE interface SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`, interfaceID)
	if err != nil {
		return fmt.Errorf("删除接口失败: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errcode.ErrNotFound
	}
	return nil
}

// ===================== 阈值 =====================

// ListThresholds 接口下阈值列表（仅未删除，按 ID 稳定排序）。
func (r *PGRepository) ListThresholds(ctx context.Context, interfaceID int64) ([]domain.Threshold, error) {
	const q = `SELECT ` + thresholdColumns + ` FROM threshold
	           WHERE interface_id = $1 AND deleted_at IS NULL ORDER BY id`
	rows, err := r.pool.Query(ctx, q, interfaceID)
	if err != nil {
		return nil, fmt.Errorf("查询阈值失败: %w", err)
	}
	defer rows.Close()

	out := make([]domain.Threshold, 0)
	for rows.Next() {
		t, err := scanThreshold(rows)
		if err != nil {
			return nil, fmt.Errorf("扫描阈值失败: %w", err)
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// ReplaceThresholds 全量替换接口阈值（单事务软删除旧记录 + 插入新记录）。
func (r *PGRepository) ReplaceThresholds(ctx context.Context, interfaceID int64, items []domain.Threshold) error {
	return r.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		if _, err := tx.Exec(ctx,
			`UPDATE threshold SET deleted_at = now() WHERE interface_id = $1 AND deleted_at IS NULL`,
			interfaceID); err != nil {
			return fmt.Errorf("清理旧阈值失败: %w", err)
		}
		for i := range items {
			item := &items[i]
			const q = `
				INSERT INTO threshold (interface_id, metric, operator, value, unit, enabled)
				VALUES ($1, $2, $3, $4, $5, $6)
				RETURNING id, version, created_at, updated_at`
			if err := tx.QueryRow(ctx, q, interfaceID, item.Metric, item.Operator, item.Value, item.Unit, item.Enabled).
				Scan(&item.ID, &item.Version, &item.CreatedAt, &item.UpdatedAt); err != nil {
				if isForeignKeyViolation(err) {
					return errcode.ErrNotFound
				}
				return fmt.Errorf("写入阈值失败: %w", err)
			}
		}
		return nil
	})
}
