package repository

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
)

// 系统设置·证书 / SSH-Host 存储（Phase 7）。
//
// 敏感字段一律存密文（DES-I-002）：DB 列为 TEXT，写入 base64(信封密文)，
// 读出后解码为 []byte 交回服务层，仓储不接触明文、不打印日志。

// certificateColumns 证书查询列（顺序与 scanCertificate 一致）。
const certificateColumns = `id, name, COALESCE(issuer, ''), valid_from, valid_to,
	COALESCE(cert_path, ''), COALESCE(key_path, ''), is_builtin, COALESCE(status, ''),
	COALESCE(associated_service, ''), version, created_at, updated_at`

// sshHostColumns SSH/Host 查询列（顺序与 scanSshHost 一致）。
const sshHostColumns = `id, type, host, COALESCE(port, 0), COALESCE(username, ''),
	COALESCE(auth_type, ''), COALESCE(key_path, ''), COALESCE(host_entries, '[]'::jsonb),
	COALESCE(description, ''), version, created_at, updated_at`

// scanCertificate 扫描一行证书（cert_path/key_path 由 base64 解码为密文）。
func scanCertificate(row pgx.Row) (*domain.Certificate, error) {
	var rec domain.Certificate
	var validFrom, validTo pgtype.Date
	var certB64, keyB64 string

	err := row.Scan(
		&rec.ID, &rec.Name, &rec.Issuer, &validFrom, &validTo,
		&certB64, &keyB64, &rec.IsBuiltin, &rec.Status,
		&rec.AssociatedService, &rec.Version, &rec.CreatedAt, &rec.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if validFrom.Valid {
		t := validFrom.Time
		rec.ValidFrom = &t
	}
	if validTo.Valid {
		t := validTo.Time
		rec.ValidTo = &t
	}
	certEnc, err := decodeSecretText(certB64)
	if err != nil {
		return nil, fmt.Errorf("解码证书内容失败: %w", err)
	}
	keyEnc, err := decodeSecretText(keyB64)
	if err != nil {
		return nil, fmt.Errorf("解码证书私钥失败: %w", err)
	}
	rec.CertEnc, rec.KeyEnc = certEnc, keyEnc
	return &rec, nil
}

// scanSshHost 扫描一行 SSH/Host（key_path 由 base64 解码，host_entries 由 JSONB 解码）。
func scanSshHost(row pgx.Row) (*domain.SshHost, error) {
	var rec domain.SshHost
	var keyB64 string
	var entries []byte

	err := row.Scan(
		&rec.ID, &rec.Type, &rec.Host, &rec.Port, &rec.Username,
		&rec.AuthType, &keyB64, &entries, &rec.Description,
		&rec.Version, &rec.CreatedAt, &rec.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	keyEnc, err := decodeSecretText(keyB64)
	if err != nil {
		return nil, fmt.Errorf("解码 SSH 私钥失败: %w", err)
	}
	rec.KeyEnc = keyEnc

	if len(entries) > 0 {
		if err := json.Unmarshal(entries, &rec.HostEntries); err != nil {
			return nil, fmt.Errorf("解析 Host 解析内容失败: %w", err)
		}
	}
	if rec.HostEntries == nil {
		rec.HostEntries = []string{}
	}
	return &rec, nil
}

// encodeSecretText 密文 → DB TEXT（base64）；空密文存 NULL。
func encodeSecretText(enc []byte) any {
	if len(enc) == 0 {
		return nil
	}
	return base64.StdEncoding.EncodeToString(enc)
}

// decodeSecretText DB TEXT（base64）→ 密文；空串返回 nil。
func decodeSecretText(raw string) ([]byte, error) {
	if raw == "" {
		return nil, nil
	}
	return base64.StdEncoding.DecodeString(raw)
}

// dateValue time.Time → DATE 参数（pgx 以 time.Time 编码，取值的年月日部分）。
func dateValue(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}

// ===================== 证书 =====================

// ListCertificates 证书列表（内置自签证书优先展示，其后按 ID 升序）。
func (r *PGRepository) ListCertificates(ctx context.Context) ([]domain.Certificate, error) {
	q := `SELECT ` + certificateColumns + ` FROM certificate
	      WHERE deleted_at IS NULL
	      ORDER BY is_builtin DESC, id`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("查询证书列表失败: %w", err)
	}
	defer rows.Close()

	list := make([]domain.Certificate, 0)
	for rows.Next() {
		rec, err := scanCertificate(rows)
		if err != nil {
			return nil, fmt.Errorf("扫描证书失败: %w", err)
		}
		list = append(list, *rec)
	}
	return list, rows.Err()
}

// GetCertificate 证书详情（不存在 → 10301）。
func (r *PGRepository) GetCertificate(ctx context.Context, certID int64) (*domain.Certificate, error) {
	q := `SELECT ` + certificateColumns + ` FROM certificate WHERE id = $1 AND deleted_at IS NULL`
	rec, err := scanCertificate(r.pool.QueryRow(ctx, q, certID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errcode.ErrNotFound
		}
		return nil, fmt.Errorf("查询证书失败: %w", err)
	}
	return rec, nil
}

// CreateCertificate 新增证书（cert/key 密文以 base64 存 TEXT）。
func (r *PGRepository) CreateCertificate(ctx context.Context, cert *domain.Certificate) error {
	const q = `
		INSERT INTO certificate (name, issuer, valid_from, valid_to, cert_path, key_path,
		                         is_builtin, status, associated_service)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, version, created_at, updated_at`

	status := cert.Status
	if status == "" {
		status = domain.CertStatusValid
	}
	err := r.pool.QueryRow(ctx, q,
		cert.Name, cert.Issuer, dateValue(cert.ValidFrom), dateValue(cert.ValidTo),
		encodeSecretText(cert.CertEnc), encodeSecretText(cert.KeyEnc),
		cert.IsBuiltin, status, cert.AssociatedService,
	).Scan(&cert.ID, &cert.Version, &cert.CreatedAt, &cert.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return errcode.ErrConflict
		}
		return fmt.Errorf("创建证书失败: %w", err)
	}
	return nil
}

// SoftDeleteCertificate 逻辑删除证书。
func (r *PGRepository) SoftDeleteCertificate(ctx context.Context, certID int64) error {
	return applySoftDelete(ctx, r.pool, "certificate", certID)
}

// UpdateCertificateValidity 重新初始化：仅更新有效期与状态快照。
func (r *PGRepository) UpdateCertificateValidity(ctx context.Context, certID int64, validFrom, validTo time.Time, status string) error {
	const q = `
		UPDATE certificate
		SET valid_from = $2, valid_to = $3, status = $4, version = version + 1, updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL`
	tag, err := r.pool.Exec(ctx, q, certID, validFrom, validTo, status)
	if err != nil {
		return fmt.Errorf("更新证书有效期失败: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errcode.ErrNotFound
	}
	return nil
}

// CountBuiltinCertificates 统计内置自签证书数量（用于启动时按需初始化）。
func (r *PGRepository) CountBuiltinCertificates(ctx context.Context) (int64, error) {
	const q = `SELECT count(*) FROM certificate WHERE is_builtin AND deleted_at IS NULL`
	var n int64
	if err := r.pool.QueryRow(ctx, q).Scan(&n); err != nil {
		return 0, fmt.Errorf("统计内置证书失败: %w", err)
	}
	return n, nil
}

// CertificateNameExists 证书名称是否已存在（同名校验 → 10302）。
func (r *PGRepository) CertificateNameExists(ctx context.Context, name string) (bool, error) {
	const q = `SELECT EXISTS (SELECT 1 FROM certificate WHERE name = $1 AND deleted_at IS NULL)`
	var exists bool
	if err := r.pool.QueryRow(ctx, q, name).Scan(&exists); err != nil {
		return false, fmt.Errorf("校验证书名称失败: %w", err)
	}
	return exists, nil
}

// ===================== SSH/Host =====================

// ListSshHosts SSH/Host 列表（关键字/类型筛选 + 分页）。
func (r *PGRepository) ListSshHosts(ctx context.Context, keyword, typ string, page, pageSize int) ([]domain.SshHost, int64, error) {
	pattern := ""
	if keyword != "" {
		pattern = "%" + keyword + "%"
	}
	const where = `WHERE deleted_at IS NULL
		AND ($1 = '' OR host ILIKE $1 OR COALESCE(description, '') ILIKE $1)
		AND ($2 = '' OR type = $2)`

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM ssh_host `+where, pattern, typ).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("统计 SSH/Host 失败: %w", err)
	}

	offset := (page - 1) * pageSize
	if offset < 0 {
		offset = 0
	}
	q := `SELECT ` + sshHostColumns + ` FROM ssh_host ` + where + ` ORDER BY id LIMIT $3 OFFSET $4`
	rows, err := r.pool.Query(ctx, q, pattern, typ, pageSize, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("查询 SSH/Host 列表失败: %w", err)
	}
	defer rows.Close()

	list := make([]domain.SshHost, 0, pageSize)
	for rows.Next() {
		rec, err := scanSshHost(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("扫描 SSH/Host 失败: %w", err)
		}
		list = append(list, *rec)
	}
	return list, total, rows.Err()
}

// GetSshHost SSH/Host 详情（不存在 → 10301）。
func (r *PGRepository) GetSshHost(ctx context.Context, sshHostID int64) (*domain.SshHost, error) {
	q := `SELECT ` + sshHostColumns + ` FROM ssh_host WHERE id = $1 AND deleted_at IS NULL`
	rec, err := scanSshHost(r.pool.QueryRow(ctx, q, sshHostID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errcode.ErrNotFound
		}
		return nil, fmt.Errorf("查询 SSH/Host 失败: %w", err)
	}
	return rec, nil
}

// CreateSshHost 新增 SSH/Host（私钥密文 base64 存 TEXT，port=0 存 NULL）。
func (r *PGRepository) CreateSshHost(ctx context.Context, host *domain.SshHost) error {
	entries, err := marshalHosts(host.HostEntries)
	if err != nil {
		return err
	}

	const q = `
		INSERT INTO ssh_host (type, host, port, username, auth_type, key_path, host_entries, description)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, version, created_at, updated_at`

	err = r.pool.QueryRow(ctx, q,
		host.Type, host.Host, nullablePort(host.Port), host.Username, host.AuthType,
		encodeSecretText(host.KeyEnc), entries, host.Description,
	).Scan(&host.ID, &host.Version, &host.CreatedAt, &host.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return errcode.ErrConflict
		}
		return fmt.Errorf("创建 SSH/Host 失败: %w", err)
	}
	return nil
}

// UpdateSshHost 修改 SSH/Host（version>0 启用乐观锁，冲突 → 10402）。
func (r *PGRepository) UpdateSshHost(ctx context.Context, host *domain.SshHost, version int64) error {
	entries, err := marshalHosts(host.HostEntries)
	if err != nil {
		return err
	}
	sets := map[string]any{
		"type":         host.Type,
		"host":         host.Host,
		"port":         nullablePort(host.Port),
		"username":     host.Username,
		"auth_type":    host.AuthType,
		"key_path":     encodeSecretText(host.KeyEnc),
		"host_entries": entries,
		"description":  host.Description,
	}
	if version > 0 {
		return optimisticUpdate(ctx, r.pool, "ssh_host", host.ID, version, sets)
	}
	return updateByID(ctx, r.pool, "ssh_host", host.ID, sets)
}

// SoftDeleteSshHost 逻辑删除 SSH/Host（引用校验在服务层完成）。
func (r *PGRepository) SoftDeleteSshHost(ctx context.Context, sshHostID int64) error {
	return applySoftDelete(ctx, r.pool, "ssh_host", sshHostID)
}

// CountEnvironmentsBySshHost 统计引用该 SSH/Host 的（未删除）环境数。
func (r *PGRepository) CountEnvironmentsBySshHost(ctx context.Context, sshHostID int64) (int64, error) {
	const q = `SELECT count(*) FROM environment WHERE ssh_host_id = $1 AND deleted_at IS NULL`
	var n int64
	if err := r.pool.QueryRow(ctx, q, sshHostID).Scan(&n); err != nil {
		return 0, fmt.Errorf("统计 SSH/Host 引用失败: %w", err)
	}
	return n, nil
}

// SshHostExists 判断 type+host+port（port=0 视为未设置）是否已存在。
func (r *PGRepository) SshHostExists(ctx context.Context, typ, host string, port int) (bool, error) {
	const q = `SELECT EXISTS (
		SELECT 1 FROM ssh_host
		WHERE type = $1 AND host = $2 AND COALESCE(port, 0) = $3 AND deleted_at IS NULL)`
	var exists bool
	if err := r.pool.QueryRow(ctx, q, typ, host, port).Scan(&exists); err != nil {
		return false, fmt.Errorf("校验 SSH/Host 重复失败: %w", err)
	}
	return exists, nil
}

// nullablePort 端口 0 → NULL（Host 解析类记录无端口）。
func nullablePort(port int) any {
	if port <= 0 {
		return nil
	}
	return port
}
