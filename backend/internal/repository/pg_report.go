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

// 报告相关查询列（顺序与 scanReport 一致）。
const reportColumns = `id, COALESCE(task_id, 0), COALESCE(product_id, 0), source,
	COALESCE(type, ''), COALESCE(title, ''), COALESCE(summary, '{}'::jsonb),
	COALESCE(metrics, '{}'::jsonb), status, version, created_at, updated_at`

// artifactColumns 产物查询列。
const artifactColumns = `id, report_id, format, path, COALESCE(size, 0), created_at`

// scanReport 扫描一行报告（summary/metrics JSONB 解码）。
func scanReport(row pgx.Row) (*domain.Report, error) {
	var (
		rep              domain.Report
		summary, metrics []byte
	)
	if err := row.Scan(&rep.ID, &rep.TaskID, &rep.ProductID, &rep.Source, &rep.Type, &rep.Title,
		&summary, &metrics, &rep.Status, &rep.Version, &rep.CreatedAt, &rep.UpdatedAt); err != nil {
		return nil, err
	}
	if len(summary) > 0 && string(summary) != "{}" && string(summary) != "null" {
		var parsed domain.ReportSummary
		if err := json.Unmarshal(summary, &parsed); err != nil {
			return nil, fmt.Errorf("解析报告摘要失败: %w", err)
		}
		rep.Summary = &parsed
	}
	rep.Metrics = normalizeRawJSON(metrics)
	return &rep, nil
}

// scanArtifact 扫描一行产物。
func scanArtifact(row pgx.Row) (*domain.ReportArtifact, error) {
	var artifact domain.ReportArtifact
	if err := row.Scan(&artifact.ID, &artifact.ReportID, &artifact.Format, &artifact.Path,
		&artifact.Size, &artifact.CreatedAt); err != nil {
		return nil, err
	}
	return &artifact, nil
}

// marshalReportSummary 序列化摘要（nil → SQL NULL）。
func marshalReportSummary(summary *domain.ReportSummary) (any, error) {
	if summary == nil {
		return nil, nil
	}
	raw, err := json.Marshal(summary)
	if err != nil {
		return nil, fmt.Errorf("序列化报告摘要失败: %w", err)
	}
	return string(raw), nil
}

// marshalOptionalJSON 序列化可选 JSONB（空 → SQL NULL）。
func marshalOptionalJSON(raw []byte) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return string(raw)
}

// ListReports 报告列表（筛选 + 分页，附带各格式产物元信息）。
func (r *PGRepository) ListReports(ctx context.Context, q domain.ReportQuery) ([]domain.Report, int64, error) {
	pattern := ""
	if q.Keyword != "" {
		pattern = "%" + q.Keyword + "%"
	}
	const filter = `
		WHERE deleted_at IS NULL
		  AND ($1 = '' OR COALESCE(title, '') ILIKE $1)
		  AND ($2 = 0 OR task_id = $2)
		  AND ($3 = 0 OR product_id = $3)
		  AND ($4 = '' OR status = $4)
		  AND ($5 = '' OR source = $5)
		  AND ($6 = '' OR type = $6 OR EXISTS (
		        SELECT 1 FROM report_artifact a
		        WHERE a.report_id = test_report.id AND a.deleted_at IS NULL AND a.format = $6))
		  AND ($7::timestamptz IS NULL OR created_at >= $7)
		  AND ($8::timestamptz IS NULL OR created_at <= $8)`

	var total int64
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM test_report`+filter,
		pattern, q.TaskID, q.ProductID, q.Status, q.Source, q.Type, q.CreatedFrom, q.CreatedTo,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("统计报告失败: %w", err)
	}

	offset := (q.Page - 1) * q.PageSize
	if offset < 0 {
		offset = 0
	}
	query := `SELECT ` + reportColumns + ` FROM test_report` + filter +
		` ORDER BY id DESC LIMIT $9 OFFSET $10`
	rows, err := r.pool.Query(ctx, query,
		pattern, q.TaskID, q.ProductID, q.Status, q.Source, q.Type, q.CreatedFrom, q.CreatedTo,
		q.PageSize, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("查询报告列表失败: %w", err)
	}
	defer rows.Close()

	out := make([]domain.Report, 0, q.PageSize)
	for rows.Next() {
		rep, err := scanReport(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("扫描报告失败: %w", err)
		}
		out = append(out, *rep)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	if err := r.attachArtifacts(ctx, out); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// attachArtifacts 批量加载报告产物并挂载到 reports（避免 N+1 查询）。
func (r *PGRepository) attachArtifacts(ctx context.Context, reports []domain.Report) error {
	if len(reports) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(reports))
	for i := range reports {
		ids = append(ids, reports[i].ID)
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+artifactColumns+` FROM report_artifact
		 WHERE deleted_at IS NULL AND report_id = ANY($1) ORDER BY report_id, id`, ids)
	if err != nil {
		return fmt.Errorf("查询报告产物失败: %w", err)
	}
	defer rows.Close()

	byReport := make(map[int64][]domain.ReportArtifact, len(reports))
	for rows.Next() {
		artifact, err := scanArtifact(rows)
		if err != nil {
			return fmt.Errorf("扫描报告产物失败: %w", err)
		}
		byReport[artifact.ReportID] = append(byReport[artifact.ReportID], *artifact)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range reports {
		reports[i].Artifacts = byReport[reports[i].ID]
	}
	return nil
}

// GetReport 报告详情（不存在 → 10301），含全部产物。
func (r *PGRepository) GetReport(ctx context.Context, id int64) (*domain.Report, error) {
	const q = `SELECT ` + reportColumns + ` FROM test_report WHERE id = $1 AND deleted_at IS NULL`
	rep, err := scanReport(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errcode.ErrNotFound
		}
		return nil, fmt.Errorf("查询报告失败: %w", err)
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+artifactColumns+` FROM report_artifact
		 WHERE deleted_at IS NULL AND report_id = $1 ORDER BY id`, id)
	if err != nil {
		return nil, fmt.Errorf("查询报告产物失败: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		artifact, err := scanArtifact(rows)
		if err != nil {
			return nil, fmt.Errorf("扫描报告产物失败: %w", err)
		}
		rep.Artifacts = append(rep.Artifacts, *artifact)
	}
	return rep, rows.Err()
}

// CreateReport 新建报告记录（状态由调用方给出，默认 generating）。
func (r *PGRepository) CreateReport(ctx context.Context, tx Tx, rep *domain.Report) error {
	if rep.Status == "" {
		rep.Status = domain.ReportStatusGenerating
	}
	if rep.Source == "" {
		rep.Source = domain.ReportSourceInternal
	}
	summary, err := marshalReportSummary(rep.Summary)
	if err != nil {
		return err
	}

	const q = `
		INSERT INTO test_report (task_id, product_id, source, type, title, summary, metrics, status)
		VALUES (NULLIF($1, 0), NULLIF($2, 0), $3, $4, $5, $6::jsonb, $7::jsonb, $8)
		RETURNING id, version, created_at, updated_at`
	if err := r.conn(tx).QueryRow(ctx, q,
		rep.TaskID, rep.ProductID, rep.Source, rep.Type, rep.Title,
		summary, marshalOptionalJSON(rep.Metrics), rep.Status,
	).Scan(&rep.ID, &rep.Version, &rep.CreatedAt, &rep.UpdatedAt); err != nil {
		if isForeignKeyViolation(err) {
			return errcode.ErrNotFound
		}
		return fmt.Errorf("创建报告失败: %w", err)
	}
	return nil
}

// UpdateReport 更新报告状态/摘要/指标（summary、metrics 为 nil 时保留原值）。
func (r *PGRepository) UpdateReport(ctx context.Context, tx Tx, id int64, status string,
	summary *domain.ReportSummary, metrics []byte) error {
	summaryJSON, err := marshalReportSummary(summary)
	if err != nil {
		return err
	}
	const q = `UPDATE test_report
	           SET status = $2, summary = COALESCE($3::jsonb, summary),
	               metrics = COALESCE($4::jsonb, metrics), version = version + 1, updated_at = now()
	           WHERE id = $1 AND deleted_at IS NULL`
	tag, err := r.conn(tx).Exec(ctx, q, id, status, summaryJSON, marshalOptionalJSON(metrics))
	if err != nil {
		return fmt.Errorf("更新报告失败: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errcode.ErrNotFound
	}
	return nil
}

// ReplaceArtifacts 全量替换报告产物（旧产物逻辑删除，新产物按 format/path/size 落库）。
func (r *PGRepository) ReplaceArtifacts(ctx context.Context, tx Tx, reportID int64, artifacts []domain.ReportArtifact) error {
	conn := r.conn(tx)
	if _, err := conn.Exec(ctx,
		`UPDATE report_artifact SET deleted_at = now() WHERE report_id = $1 AND deleted_at IS NULL`,
		reportID); err != nil {
		return fmt.Errorf("清理旧报告产物失败: %w", err)
	}
	const insertQ = `INSERT INTO report_artifact (report_id, format, path, size)
	                 VALUES ($1, $2, $3, $4) RETURNING id, created_at`
	for i := range artifacts {
		artifact := &artifacts[i]
		artifact.ReportID = reportID
		if err := conn.QueryRow(ctx, insertQ, reportID, artifact.Format, artifact.Path, artifact.Size).
			Scan(&artifact.ID, &artifact.CreatedAt); err != nil {
			return fmt.Errorf("写入报告产物失败: %w", err)
		}
	}
	return nil
}
