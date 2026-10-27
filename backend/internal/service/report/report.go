// Package report 报告管理业务服务（生成/导入/导出/在线查看）。
//
// 对齐 PRD §3.4/§4.9、api-contract §2.5 与安全设计：
//   - 任务完成后自动全量生成 html/pdf/word（决策 #4），失败可重新生成（PRD §7.3）；
//   - 产物路径只来自 DB 记录，读取前 filepath.Clean + 白名单目录前缀校验（DES-T-002）；
//   - 导入报告扩展名 + magic bytes 双校验 + 大小限制 + 随机文件名（DES-T-001）；
//   - 报告渲染前对接口地址脱敏，认证头不进入报告（DES-I-001 / INV-7）。
package report

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
	"auto_performance/internal/evaluator"
	"auto_performance/internal/reportgen"
	"auto_performance/internal/reportgen/word"
	"auto_performance/internal/repository"
)

const (
	// DefaultPageSize 列表默认分页大小（api-contract §1.3）。
	DefaultPageSize = 20
	// MaxPageSize 列表分页上限。
	MaxPageSize = 200
	// DefaultMaxImportSize 导入大小上限缺省值（50MB，配置项 report.max_size_bytes）。
	DefaultMaxImportSize int64 = 50 * 1024 * 1024
	// 报告标题长度上限（test_report.title VARCHAR(256)）。
	maxTitleLen = 256
	// statusUpdateTimeout 异步/异常路径回写状态的超时。
	statusUpdateTimeout = 10 * time.Second
)

// Repo 报告仓储能力（由 repository.PGRepository 实现；测试注入内存假仓储）。
type Repo interface {
	WithTx(ctx context.Context, fn repository.TxFunc) error
	ListReports(ctx context.Context, q domain.ReportQuery) ([]domain.Report, int64, error)
	GetReport(ctx context.Context, id int64) (*domain.Report, error)
	CreateReport(ctx context.Context, tx repository.Tx, rep *domain.Report) error
	UpdateReport(ctx context.Context, tx repository.Tx, id int64, status string,
		summary *domain.ReportSummary, metrics []byte) error
	ReplaceArtifacts(ctx context.Context, tx repository.Tx, reportID int64, artifacts []domain.ReportArtifact) error
	GetTask(ctx context.Context, id int64) (*domain.TestTask, error)
}

// HTMLRenderer 渲染 HTML 报告（默认 reportgen.RenderHTML；测试可注入桩）。
type HTMLRenderer func(w io.Writer, data *reportgen.ReportData) error

// WordRenderer 渲染 Word 报告（默认 word.Generate；测试可注入桩）。
type WordRenderer func(w io.Writer, data *reportgen.ReportData) error

// PDFRenderer PDF 打印能力（默认 internal/reportgen/pdf.Generator）。
type PDFRenderer interface {
	Generate(ctx context.Context, htmlPath, outPath string) error
}

// Options 服务依赖与配置。
type Options struct {
	// Dir 报告产物根目录（security.report_dir）；所有产物必须落在该目录内。
	Dir string
	// MaxImportSize 导入文件大小上限（report.max_size_bytes），<=0 取默认 50MB。
	MaxImportSize int64
	// HTML/Word/PDF 渲染器（为空时使用默认实现）。
	HTML HTMLRenderer
	Word WordRenderer
	PDF  PDFRenderer
	// Clock 时钟（测试可注入）。
	Clock func() time.Time
}

// Service 报告管理业务服务。
type Service struct {
	repo          Repo
	dir           string
	maxImportSize int64
	renderHTML    HTMLRenderer
	renderWord    WordRenderer
	pdf           PDFRenderer
	now           func() time.Time
}

// New 构造报告服务（报告目录自动创建；dir 必填）。
func New(repo Repo, opts Options) (*Service, error) {
	dir := strings.TrimSpace(opts.Dir)
	if dir == "" {
		return nil, fmt.Errorf("报告目录未配置（security.report_dir）")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("解析报告目录失败: %w", err)
	}
	abs = filepath.Clean(abs)
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return nil, fmt.Errorf("创建报告目录失败: %w", err)
	}

	svc := &Service{
		repo:          repo,
		dir:           abs,
		maxImportSize: opts.MaxImportSize,
		renderHTML:    opts.HTML,
		renderWord:    opts.Word,
		pdf:           opts.PDF,
		now:           opts.Clock,
	}
	if svc.maxImportSize <= 0 {
		svc.maxImportSize = DefaultMaxImportSize
	}
	if svc.renderHTML == nil {
		svc.renderHTML = reportgen.RenderHTML
	}
	if svc.renderWord == nil {
		svc.renderWord = word.Generate
	}
	if svc.now == nil {
		svc.now = time.Now
	}
	return svc, nil
}

// Dir 返回报告根目录（测试与审计使用）。
func (s *Service) Dir() string {
	if s == nil {
		return ""
	}
	return s.dir
}

// MaxImportSize 返回导入大小上限。
func (s *Service) MaxImportSize() int64 {
	if s == nil || s.maxImportSize <= 0 {
		return DefaultMaxImportSize
	}
	return s.maxImportSize
}

// ===================== DTO =====================

// View 报告视图（ID 由接入层转字符串）。
type View struct {
	ID        int64
	TaskID    int64
	ProductID int64
	Source    string
	Type      string
	Title     string
	Status    string
	Summary   *domain.ReportSummary
	Artifacts []domain.ReportArtifact
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Page 报告分页结果。
type Page struct {
	List     []View
	Total    int64
	Page     int
	PageSize int
}

// ListQuery 报告列表查询条件。
type ListQuery struct {
	TaskID      int64
	ProductID   int64
	Type        string
	Status      string
	Source      string
	Keyword     string
	CreatedFrom *time.Time
	CreatedTo   *time.Time
	Page        int
	PageSize    int
}

// ArtifactFile 待下发的产物文件（路径已通过白名单目录校验）。
type ArtifactFile struct {
	Path        string
	Format      string
	Filename    string
	ContentType string
	Size        int64
	// Inline true：html/pdf（在线查看）；false：word（下载）。
	Inline bool
	Report *View
}

// ===================== 查询 =====================

// List 报告列表（筛选 + 分页）。
func (s *Service) List(ctx context.Context, q ListQuery) (*Page, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	reportType := strings.ToLower(strings.TrimSpace(q.Type))
	if reportType != "" && !domain.IsValidReportFormat(reportType) {
		return nil, errcode.ErrParamFmt
	}
	status := strings.ToLower(strings.TrimSpace(q.Status))
	if status != "" && !domain.IsValidReportStatus(status) {
		return nil, errcode.ErrParamFmt
	}
	source := strings.ToLower(strings.TrimSpace(q.Source))
	if source != "" && !domain.IsValidReportSource(source) {
		return nil, errcode.ErrParamFmt
	}
	if q.TaskID < 0 || q.ProductID < 0 {
		return nil, errcode.ErrParamFmt
	}
	if q.CreatedFrom != nil && q.CreatedTo != nil && q.CreatedFrom.After(*q.CreatedTo) {
		return nil, errcode.ErrParamFmt
	}

	page, pageSize := normalizePaging(q.Page, q.PageSize)
	reports, total, err := s.repo.ListReports(ctx, domain.ReportQuery{
		TaskID:      q.TaskID,
		ProductID:   q.ProductID,
		Type:        reportType,
		Status:      status,
		Source:      source,
		Keyword:     strings.TrimSpace(q.Keyword),
		CreatedFrom: q.CreatedFrom,
		CreatedTo:   q.CreatedTo,
		Page:        page,
		PageSize:    pageSize,
	})
	if err != nil {
		return nil, err
	}
	list := make([]View, 0, len(reports))
	for i := range reports {
		list = append(list, toView(&reports[i]))
	}
	return &Page{List: list, Total: total, Page: page, PageSize: pageSize}, nil
}

// Get 报告详情（不存在 → 10301）。
func (s *Service) Get(ctx context.Context, id int64) (*View, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	if id <= 0 {
		return nil, errcode.ErrParamFmt
	}
	rep, err := s.repo.GetReport(ctx, id)
	if err != nil {
		return nil, err
	}
	view := toView(rep)
	return &view, nil
}

// ===================== 生成 / 重新生成 =====================

// Regenerate 重新生成报告（PRD §7.3：generation_failed / ready → generating）。
//
// 仅内部生成且归属任务的报告可重新生成；外部导入报告无任务数据 → 24004。
func (s *Service) Regenerate(ctx context.Context, id int64) (*View, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	if id <= 0 {
		return nil, errcode.ErrParamFmt
	}
	rep, err := s.repo.GetReport(ctx, id)
	if err != nil {
		return nil, err
	}
	if !domain.CanRegenerateReport(rep.Status) {
		return nil, errcode.ErrState
	}
	if rep.TaskID <= 0 {
		return nil, fmt.Errorf("%w: 外部导入报告无任务执行数据，无法重新生成", ErrReportGenerateFailed)
	}
	task, err := s.repo.GetTask(ctx, rep.TaskID)
	if err != nil {
		return nil, err
	}

	if err := s.updateStatus(ctx, rep.ID, domain.ReportStatusGenerating, nil, nil); err != nil {
		return nil, err
	}
	if err := s.generate(ctx, rep, task, nil); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// Generate 实现编排器 ReportGenerator：任务完成后自动全量生成报告。
//
// 报告记录先以 generating 落库（前端可见进度），随后渲染产物；失败标记
// generation_failed（可重新生成）并向编排器返回错误（任务进入失败态）。
func (s *Service) Generate(ctx context.Context, task *domain.TestTask, evaluation evaluator.Evaluation) error {
	if s == nil || s.repo == nil {
		return errcode.ErrDependency
	}
	if task == nil || task.ID <= 0 {
		return fmt.Errorf("%w: 任务为空", ErrReportGenerateFailed)
	}
	// 复用最新任务记录（含执行结果 result，供报告读取指标/评估）。
	if fresh, err := s.repo.GetTask(ctx, task.ID); err == nil {
		task = fresh
	} else if !errors.Is(err, errcode.ErrNotFound) {
		return err
	}

	formats, err := taskFormats(task)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrReportGenerateFailed, err)
	}
	rep := &domain.Report{
		TaskID:    task.ID,
		ProductID: firstID(task.ProductIDs),
		Source:    domain.ReportSourceInternal,
		Type:      formats[0],
		Title:     reportTitle(task, s.now()),
		Status:    domain.ReportStatusGenerating,
	}
	if err := s.repo.CreateReport(ctx, nil, rep); err != nil {
		return err
	}
	eval := evaluation
	return s.generate(ctx, rep, task, &eval)
}

// generate 渲染并落库产物（失败时标记 generation_failed）。
func (s *Service) generate(ctx context.Context, rep *domain.Report, task *domain.TestTask, fallback *evaluator.Evaluation) error {
	formats, err := taskFormats(task)
	if err != nil {
		s.markFailed(ctx, rep.ID)
		return fmt.Errorf("%w: %v", ErrReportGenerateFailed, err)
	}
	data := s.buildData(rep, task, fallback)
	artifacts, failedFormats, err := s.renderArtifacts(ctx, rep, formats, data)
	if err != nil {
		s.markFailed(ctx, rep.ID)
		return fmt.Errorf("%w: %v", ErrReportGenerateFailed, err)
	}

	metrics, err := json.Marshal(data.Summary.Metrics)
	if err != nil {
		metrics = nil
	}
	summary := data.Summary
	if len(failedFormats) > 0 {
		// 部分格式失败：报告仍标记 ready，失败格式记录到 summary 供展示。
		summary.FailedFormats = failedFormats
		log.Printf("报告 %d 部分格式生成失败（不影响其他格式）: %v", rep.ID, failedFormats)
	}
	err = s.repo.WithTx(ctx, func(ctx context.Context, tx repository.Tx) error {
		if err := s.repo.UpdateReport(ctx, tx, rep.ID, domain.ReportStatusReady, &summary, metrics); err != nil {
			return err
		}
		return s.repo.ReplaceArtifacts(ctx, tx, rep.ID, artifacts)
	})
	if err != nil {
		s.markFailed(ctx, rep.ID)
		return err
	}
	rep.Status = domain.ReportStatusReady
	rep.Summary = &summary
	rep.Artifacts = artifacts
	return nil
}

// buildData 组装渲染模型（指标/评估来自任务执行结果，标题与环境来自任务与报告记录）。
func (s *Service) buildData(rep *domain.Report, task *domain.TestTask, fallback *evaluator.Evaluation) *reportgen.ReportData {
	metrics, duration, evaluation := parseTaskResult(task)
	if fallback != nil && len(evaluation.Items) == 0 {
		evaluation = *fallback
	}
	title := strings.TrimSpace(rep.Title)
	if title == "" {
		title = reportTitle(task, s.now())
	}
	environment := "-"
	if task != nil && task.EnvID > 0 {
		environment = fmt.Sprintf("环境 #%d", task.EnvID)
	}
	taskName := ""
	if task != nil {
		taskName = task.Name
	}
	return reportgen.BuildData(reportgen.BuildInput{
		ReportID:        rep.ID,
		TaskID:          rep.TaskID,
		Title:           title,
		TaskName:        taskName,
		Environment:     environment,
		Source:          rep.Source,
		GeneratedAt:     s.now(),
		DurationSeconds: duration,
		Metrics:         metrics,
		Evaluation:      evaluation,
		Interfaces:      task.InterfaceSnapshots,
	})
}

// renderArtifacts 按格式渲染产物文件（目录为 <reportDir>/<reportID>，全部由服务端生成）。
//
// 各格式独立生成：某一格式失败（如未装 Chromium 导致 PDF 失败）不影响其他格式，
// 失败格式记入 failed 返回；仅当所有格式均失败时返回错误。
func (s *Service) renderArtifacts(ctx context.Context, rep *domain.Report, formats []string,
	data *reportgen.ReportData) (artifacts []domain.ReportArtifact, failed []string, err error) {
	dir := filepath.Join(s.dir, strconv.FormatInt(rep.ID, 10))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, nil, fmt.Errorf("创建报告产物目录失败: %w", err)
	}

	htmlPath := filepath.Join(dir, "report.html")
	wantHTML := containsFormat(formats, domain.ReportFormatHTML)
	wantPDF := containsFormat(formats, domain.ReportFormatPDF)
	wantWord := containsFormat(formats, domain.ReportFormatWord)

	artifacts = make([]domain.ReportArtifact, 0, len(formats))
	htmlOK := false
	// PDF 由 HTML 打印而来：PDF 单格式时也生成中间 HTML（打印后删除）。
	if wantHTML || wantPDF {
		size, herr := s.writeArtifact(htmlPath, func(w io.Writer) error {
			return s.renderHTML(w, data)
		})
		if herr != nil {
			log.Printf("报告 HTML 渲染失败: %v", herr)
			failed = append(failed, domain.ReportFormatHTML)
			if wantPDF {
				failed = append(failed, domain.ReportFormatPDF)
			}
		} else {
			htmlOK = true
			if wantHTML {
				artifacts = append(artifacts, newArtifact(rep.ID, domain.ReportFormatHTML, htmlPath, size))
			}
		}
	}

	if wantPDF && htmlOK {
		if s.pdf == nil {
			log.Printf("报告 PDF 生成器未配置，跳过 PDF")
			failed = append(failed, domain.ReportFormatPDF)
		} else {
			pdfPath := filepath.Join(dir, "report.pdf")
			if perr := s.pdf.Generate(ctx, htmlPath, pdfPath); perr != nil {
				log.Printf("报告 PDF 生成失败: %v", perr)
				failed = append(failed, domain.ReportFormatPDF)
			} else if psize, serr := fileSize(pdfPath); serr != nil {
				log.Printf("报告 PDF 产物读取失败: %v", serr)
				failed = append(failed, domain.ReportFormatPDF)
			} else {
				artifacts = append(artifacts, newArtifact(rep.ID, domain.ReportFormatPDF, pdfPath, psize))
			}
		}
	}

	if wantWord {
		if s.renderWord == nil {
			log.Printf("报告 Word 生成器未配置，跳过 Word")
			failed = append(failed, domain.ReportFormatWord)
		} else {
			wordPath := filepath.Join(dir, "report.docx")
			wsize, werr := s.writeArtifact(wordPath, func(w io.Writer) error {
				return s.renderWord(w, data)
			})
			if werr != nil {
				log.Printf("报告 Word 生成失败: %v", werr)
				failed = append(failed, domain.ReportFormatWord)
			} else {
				artifacts = append(artifacts, newArtifact(rep.ID, domain.ReportFormatWord, wordPath, wsize))
			}
		}
	}

	// PDF 单格式：中间 HTML 不对外暴露，打印后立即删除。
	if !wantHTML && wantPDF && htmlOK {
		_ = os.Remove(htmlPath)
	}

	if len(artifacts) == 0 {
		return nil, failed, fmt.Errorf("所有报告格式均生成失败: %v", failed)
	}
	return artifacts, failed, nil
}

// writeArtifact 原子写产物（先写 .tmp 再 rename；权限 0640，禁止可执行）。
func (s *Service) writeArtifact(path string, render func(io.Writer) error) (int64, error) {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return 0, fmt.Errorf("创建报告产物失败: %w", err)
	}
	bw := bufio.NewWriter(f)
	renderErr := render(bw)
	if renderErr == nil {
		renderErr = bw.Flush()
	}
	closeErr := f.Close()
	if renderErr != nil {
		_ = os.Remove(tmp)
		return 0, fmt.Errorf("渲染报告失败: %w", renderErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return 0, fmt.Errorf("写入报告产物失败: %w", closeErr)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return 0, fmt.Errorf("保存报告产物失败: %w", err)
	}
	return fileSize(path)
}

// markFailed 标记报告生成失败（独立上下文：请求/任务取消后仍可回写状态）。
func (s *Service) markFailed(ctx context.Context, reportID int64) {
	updateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), statusUpdateTimeout)
	defer cancel()
	if err := s.updateStatus(updateCtx, reportID, domain.ReportStatusGenerationFailed, nil, nil); err != nil {
		fmt.Fprintf(os.Stderr, "[report] 回写报告失败状态失败 report=%d: %v\n", reportID, err)
	}
}

// updateStatus 状态回写（summary/metrics 为 nil 时保留原值）。
func (s *Service) updateStatus(ctx context.Context, reportID int64, status string,
	summary *domain.ReportSummary, metrics []byte) error {
	return s.repo.WithTx(ctx, func(ctx context.Context, tx repository.Tx) error {
		return s.repo.UpdateReport(ctx, tx, reportID, status, summary, metrics)
	})
}

// ===================== 在线查看 / 导出 =====================

// Artifact 解析报告产物（DES-T-002：路径源 DB + Clean + 白名单目录前缀校验）。
//
// 不存在/越界/非普通文件一律返回 24001（统一口径，避免泄露文件系统信息）。
func (s *Service) Artifact(ctx context.Context, id int64, format string) (*ArtifactFile, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	if id <= 0 {
		return nil, errcode.ErrParamFmt
	}
	format = strings.ToLower(strings.TrimSpace(format))
	if !domain.IsValidReportFormat(format) {
		return nil, ErrReportFileNotFound
	}
	rep, err := s.repo.GetReport(ctx, id)
	if err != nil {
		if errors.Is(err, errcode.ErrNotFound) {
			return nil, ErrReportFileNotFound
		}
		return nil, err
	}

	for _, artifact := range rep.Artifacts {
		if artifact.Format != format {
			continue
		}
		path, size, err := s.safeArtifactPath(artifact.Path)
		if err != nil {
			return nil, err
		}
		view := toView(rep)
		return &ArtifactFile{
			Path:        path,
			Format:      format,
			Filename:    fmt.Sprintf("report-%d%s", rep.ID, domain.ReportExtension(format)),
			ContentType: contentType(format),
			Size:        size,
			Inline:      format != domain.ReportFormatWord,
			Report:      &view,
		}, nil
	}
	return nil, ErrReportFileNotFound
}

// safeArtifactPath 校验产物路径落在报告根目录内且为普通文件。
func (s *Service) safeArtifactPath(raw string) (string, int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsRune(raw, 0) {
		return "", 0, ErrReportFileNotFound
	}
	clean := filepath.Clean(raw)
	if !filepath.IsAbs(clean) || !s.withinRoot(clean) {
		return "", 0, ErrReportFileNotFound
	}
	// 符号链接解析后必须仍在根目录内（防止通过 symlink 逃逸）。
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		if !s.withinRoot(resolved) {
			return "", 0, ErrReportFileNotFound
		}
		clean = resolved
	}
	info, err := os.Stat(clean)
	if err != nil || !info.Mode().IsRegular() {
		return "", 0, ErrReportFileNotFound
	}
	return clean, info.Size(), nil
}

// withinRoot 判断路径是否位于报告根目录之内（不含根目录自身）。
func (s *Service) withinRoot(path string) bool {
	root := filepath.Clean(s.dir)
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		root = strings.ToLower(root)
		path = strings.ToLower(path)
	}
	if path == root {
		return false
	}
	return strings.HasPrefix(path, root+string(os.PathSeparator))
}

// ===================== helpers =====================

// parseTaskResult 解析任务执行结果（指标/用时/评估），供报告渲染。
func parseTaskResult(task *domain.TestTask) (map[evaluator.Metric]float64, float64, evaluator.Evaluation) {
	metrics := map[evaluator.Metric]float64{}
	if task == nil || len(task.Result) == 0 {
		return metrics, 0, evaluator.Evaluation{}
	}
	var payload struct {
		DurationSeconds float64              `json:"duration_seconds"`
		Metrics         map[string]float64   `json:"metrics"`
		Evaluation      evaluator.Evaluation `json:"evaluation"`
	}
	if err := json.Unmarshal(task.Result, &payload); err != nil {
		return metrics, 0, evaluator.Evaluation{}
	}
	for key, value := range payload.Metrics {
		metrics[evaluator.Metric(key)] = value
	}
	return metrics, payload.DurationSeconds, payload.Evaluation
}

// taskFormats 解析任务报告配置格式（缺省全量 html/pdf/word）。
func taskFormats(task *domain.TestTask) ([]string, error) {
	if task == nil || len(task.ReportConfig) == 0 {
		return domain.ReportFormats(), nil
	}
	_, cfg, err := domain.ParseReportConfig(task.ReportConfig)
	if err != nil || cfg == nil || len(cfg.Formats) == 0 {
		return domain.ReportFormats(), nil
	}
	return cfg.Formats, nil
}

// reportTitle 生成报告标题（任务名 + 生成时间，截断到 256 字符）。
func reportTitle(task *domain.TestTask, at time.Time) string {
	name := "性能测试"
	if task != nil && strings.TrimSpace(task.Name) != "" {
		name = strings.TrimSpace(task.Name)
	}
	title := fmt.Sprintf("%s 性能测试报告（%s）", name, at.Local().Format("2006-01-02 15:04:05"))
	runes := []rune(title)
	if len(runes) > maxTitleLen {
		return string(runes[:maxTitleLen])
	}
	return title
}

// firstID 返回首个有效 ID（无则 0）。
func firstID(ids []int64) int64 {
	for _, id := range ids {
		if id > 0 {
			return id
		}
	}
	return 0
}

// containsFormat 判断格式是否在配置中。
func containsFormat(formats []string, want string) bool {
	for _, format := range formats {
		if format == want {
			return true
		}
	}
	return false
}

// newArtifact 构造产物记录。
func newArtifact(reportID int64, format, path string, size int64) domain.ReportArtifact {
	return domain.ReportArtifact{ReportID: reportID, Format: format, Path: path, Size: size}
}

// fileSize 读取文件大小。
func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("读取报告产物失败: %w", err)
	}
	return info.Size(), nil
}

// contentType 格式 → 响应 Content-Type。
func contentType(format string) string {
	switch format {
	case domain.ReportFormatHTML:
		return "text/html; charset=utf-8"
	case domain.ReportFormatPDF:
		return "application/pdf"
	case domain.ReportFormatWord:
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	default:
		return "application/octet-stream"
	}
}

// toView 领域报告 → 视图。
func toView(rep *domain.Report) View {
	return View{
		ID:        rep.ID,
		TaskID:    rep.TaskID,
		ProductID: rep.ProductID,
		Source:    rep.Source,
		Type:      rep.Type,
		Title:     rep.Title,
		Status:    rep.Status,
		Summary:   rep.Summary,
		Artifacts: append([]domain.ReportArtifact{}, rep.Artifacts...),
		CreatedAt: rep.CreatedAt,
		UpdatedAt: rep.UpdatedAt,
	}
}

// normalizePaging 归一化分页（默认 20，上限 200）。
func normalizePaging(page, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = DefaultPageSize
	}
	if pageSize > MaxPageSize {
		pageSize = MaxPageSize
	}
	return page, pageSize
}
