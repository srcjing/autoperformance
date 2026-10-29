// Package setting 系统设置业务服务（管理员/角色/证书/SSH-Host）。
//
// 本文件实现 SSH/Host 管理（Phase 7 任务 7.2）：对齐 PRD §4.13、api-contract §2.7；
// SSH 私钥 AES-256-GCM 信封加密落库（DES-I-002），响应仅返回 HasKey；
// 主机/参数严格白名单校验，阻断命令注入（DES-T-003）。
package setting

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
	reportsvc "auto_performance/internal/service/report"
)

const (
	// SshHostMaxImportBytes 批量导入文件大小上限（2 MiB）。
	SshHostMaxImportBytes int64 = 2 << 20
	// SshHostDefaultPort SSH 默认端口。
	SshHostDefaultPort = 22
	// importErrorSamples 导入结果最多回传的错误样例条数（避免响应过大）。
	importErrorSamples = 10
)

// ErrUploadTypeNotAllowed 上传文件类型不允许：api-contract §2.7 对 SSH/Host 导入沿用
// 报告模块的 24002。错误码注册全局唯一，这里直接引用既有实例（重复注册会 panic）。
var ErrUploadTypeNotAllowed = reportsvc.ErrReportTypeNotAllowed

// SshHostRepo SSH/Host 仓储能力（由 repository.PGRepository 实现）。
type SshHostRepo interface {
	ListSshHosts(ctx context.Context, keyword, typ string, page, pageSize int) ([]domain.SshHost, int64, error)
	GetSshHost(ctx context.Context, sshHostID int64) (*domain.SshHost, error)
	CreateSshHost(ctx context.Context, host *domain.SshHost) error
	UpdateSshHost(ctx context.Context, host *domain.SshHost, version int64) error
	SoftDeleteSshHost(ctx context.Context, sshHostID int64) error
	// CountEnvironmentsBySshHost 统计引用该记录的（未删除）环境数（删除保护 26005）。
	CountEnvironmentsBySshHost(ctx context.Context, sshHostID int64) (int64, error)
	// SshHostExists 判断 type+host+port 是否已存在（导入去重）。
	SshHostExists(ctx context.Context, typ, host string, port int) (bool, error)
}

// ListSshHostsQuery SSH/Host 列表查询（关键字 + 类型 + 分页）。
type ListSshHostsQuery struct {
	Keyword  string
	Type     string
	Page     int
	PageSize int
}

// SshHostView SSH/Host 视图（私钥脱敏为 HasKey 标记）。
type SshHostView struct {
	ID          int64
	Type        string
	Host        string
	Port        int
	User        string
	AuthType    string
	HasKey      bool
	HostEntries []string
	Description string
	Version     int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// SshHostPage SSH/Host 分页结果（对齐 api-contract §1.3）。
type SshHostPage struct {
	List     []SshHostView
	Total    int64
	Page     int
	PageSize int
}

// SshHostInput SSH/Host 新建/修改输入（白名单字段；Key 为私钥明文，仅入参不落库）。
type SshHostInput struct {
	Type        string
	Host        string
	Port        int
	User        string
	AuthType    string
	Key         string
	HostEntries []string
	Description string
	Version     int64
}

// SshHostImportResult 批量导入结果（重复项跳过，非法行忽略并回报样例）。
type SshHostImportResult struct {
	Total   int
	Created int
	Skipped int
	Invalid int
	Errors  []string
}

// ListSshHosts SSH/Host 列表（关键字/类型筛选 + 分页）。
func (s *Service) ListSshHosts(ctx context.Context, q ListSshHostsQuery) (*SshHostPage, error) {
	if s == nil || s.hosts == nil {
		return nil, errcode.ErrDependency
	}
	page, pageSize := normalizePaging(q.Page, q.PageSize)
	q.Page, q.PageSize = page, pageSize
	q.Keyword = strings.TrimSpace(q.Keyword)
	if q.Type != "" && q.Type != domain.SshHostTypeHost && q.Type != domain.SshHostTypeSSH {
		return nil, errcode.ErrParamFmt
	}

	records, total, err := s.hosts.ListSshHosts(ctx, q.Keyword, q.Type, page, pageSize)
	if err != nil {
		return nil, err
	}
	list := make([]SshHostView, 0, len(records))
	for i := range records {
		list = append(list, toSshHostView(&records[i]))
	}
	return &SshHostPage{List: list, Total: total, Page: page, PageSize: pageSize}, nil
}

// CreateSshHost 新增 SSH/Host（私钥信封加密存储；同 type+host+port 重复 → 10302）。
func (s *Service) CreateSshHost(ctx context.Context, in SshHostInput) (*SshHostView, error) {
	if s == nil || s.hosts == nil {
		return nil, errcode.ErrDependency
	}

	rec, err := buildSshHost(in, nil)
	if err != nil {
		return nil, err
	}

	keyEnc, err := s.encryptSshKey(rec, in.Key, nil)
	if err != nil {
		return nil, err
	}
	rec.KeyEnc = keyEnc

	exists, err := s.hosts.SshHostExists(ctx, rec.Type, rec.Host, rec.Port)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, errcode.ErrConflict
	}
	if err := s.hosts.CreateSshHost(ctx, rec); err != nil {
		return nil, err
	}
	view := toSshHostView(rec)
	return &view, nil
}

// UpdateSshHost 修改 SSH/Host（version>0 启用乐观锁 → 10402；私钥留空表示保持不变）。
func (s *Service) UpdateSshHost(ctx context.Context, sshHostID int64, in SshHostInput) (*SshHostView, error) {
	if s == nil || s.hosts == nil {
		return nil, errcode.ErrDependency
	}
	if sshHostID <= 0 {
		return nil, errcode.ErrParamFmt
	}

	current, err := s.hosts.GetSshHost(ctx, sshHostID)
	if err != nil {
		return nil, err
	}
	rec, err := buildSshHost(in, current)
	if err != nil {
		return nil, err
	}
	rec.ID = current.ID

	// 私钥：显式传入则重新加密；未传入时沿用原密文（避免前端回显密钥）。
	keyEnc, err := s.encryptSshKey(rec, in.Key, current)
	if err != nil {
		return nil, err
	}
	rec.KeyEnc = keyEnc

	if err := s.hosts.UpdateSshHost(ctx, rec, in.Version); err != nil {
		return nil, err
	}
	saved, err := s.hosts.GetSshHost(ctx, sshHostID)
	if err != nil {
		return nil, err
	}
	view := toSshHostView(saved)
	return &view, nil
}

// DeleteSshHost 删除 SSH/Host；被环境引用 → 26005。
func (s *Service) DeleteSshHost(ctx context.Context, sshHostID int64) error {
	if s == nil || s.hosts == nil {
		return errcode.ErrDependency
	}
	if sshHostID <= 0 {
		return errcode.ErrParamFmt
	}
	if _, err := s.hosts.GetSshHost(ctx, sshHostID); err != nil {
		return err
	}
	count, err := s.hosts.CountEnvironmentsBySshHost(ctx, sshHostID)
	if err != nil {
		return err
	}
	if count > 0 {
		return ErrSshHostReferenced
	}
	return s.hosts.SoftDeleteSshHost(ctx, sshHostID)
}

// ImportSshHosts 批量导入 SSH/Host（JSON/CSV，逐行解析，重复项跳过）。
func (s *Service) ImportSshHosts(ctx context.Context, filename string, size int64, content io.Reader) (*SshHostImportResult, error) {
	if s == nil || s.hosts == nil {
		return nil, errcode.ErrDependency
	}
	if size > SshHostMaxImportBytes {
		return nil, errcode.ErrParamFmt
	}

	data, err := io.ReadAll(io.LimitReader(content, SshHostMaxImportBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读取导入文件失败: %w", err)
	}
	if int64(len(data)) > SshHostMaxImportBytes {
		return nil, errcode.ErrParamFmt
	}

	rows, err := parseSshHostImportRows(filename, data)
	if err != nil {
		return nil, err
	}

	result := &SshHostImportResult{Total: len(rows), Errors: make([]string, 0, importErrorSamples)}
	seen := make(map[string]bool, len(rows))
	for i := range rows {
		rowNo := i + 1
		rec, err := buildSshHost(rows[i].toInput(), nil)
		if err != nil {
			result.Invalid++
			result.addError(fmt.Sprintf("第 %d 行：%v", rowNo, err))
			continue
		}

		key := sshHostDedupeKey(rec.Type, rec.Host, rec.Port)
		if seen[key] {
			result.Skipped++
			continue
		}
		exists, err := s.hosts.SshHostExists(ctx, rec.Type, rec.Host, rec.Port)
		if err != nil {
			return nil, err
		}
		if exists {
			seen[key] = true
			result.Skipped++
			continue
		}

		keyEnc, err := s.encryptSshKey(rec, rows[i].Key, nil)
		if err != nil {
			// 单行私钥非法：计入非法行，不阻断其余记录。
			result.Invalid++
			result.addError(fmt.Sprintf("第 %d 行：私钥非法或不可加密", rowNo))
			continue
		}
		rec.KeyEnc = keyEnc

		if err := s.hosts.CreateSshHost(ctx, rec); err != nil {
			if errors.Is(err, errcode.ErrConflict) {
				result.Skipped++
				continue
			}
			return nil, err
		}
		seen[key] = true
		result.Created++
	}

	if result.Total == 0 {
		return nil, errcode.ErrParamFmt
	}
	return result, nil
}

// addError 追加错误样例（上限 importErrorSamples）。
func (r *SshHostImportResult) addError(msg string) {
	if len(r.Errors) < importErrorSamples {
		r.Errors = append(r.Errors, msg)
	}
}

// encryptSshKey 按认证方式处理私钥：key 认证必须能取到明文（新增/或已有密文），
// password/Host 类型清空私钥。返回的密文才允许落库。
func (s *Service) encryptSshKey(rec *domain.SshHost, plainKey string, current *domain.SshHost) ([]byte, error) {
	plainKey = strings.TrimSpace(plainKey)

	if rec.Type == domain.SshHostTypeSSH && rec.AuthType == domain.SshAuthTypeKey {
		if plainKey == "" {
			if current != nil && current.HasKey() {
				return current.KeyEnc, nil // 保持原私钥（前端不回显密钥）
			}
			return nil, errcode.ErrParamMiss
		}
		if err := validateSshPrivateKey(plainKey); err != nil {
			return nil, errcode.ErrParamFmt
		}
		// 归一化为「去首尾空白 + 单个换行结尾」，保证 PEM 结构完整后再加密。
		return s.encryptSecret([]byte(strings.TrimSpace(plainKey) + "\n"))
	}

	// 非密钥认证（password / Host 解析）不保存私钥。
	return nil, nil
}

// buildSshHost 校验并归一化输入 → 领域实体（current 非空表示修改场景）。
func buildSshHost(in SshHostInput, current *domain.SshHost) (*domain.SshHost, error) {
	typ := strings.ToLower(strings.TrimSpace(in.Type))
	host := strings.TrimSpace(in.Host)
	if typ == "" || host == "" {
		return nil, errcode.ErrParamMiss
	}
	if typ != domain.SshHostTypeHost && typ != domain.SshHostTypeSSH {
		return nil, errcode.ErrParamFmt
	}
	if !validHostToken(host) {
		return nil, errcode.ErrParamFmt
	}

	rec := &domain.SshHost{
		Type:        typ,
		Host:        host,
		Description: strings.TrimSpace(in.Description),
	}
	if len(rec.Description) > 500 {
		return nil, errcode.ErrParamFmt
	}

	entries := make([]string, 0, len(in.HostEntries))
	for _, raw := range in.HostEntries {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		if !validHostEntry(entry) {
			return nil, errcode.ErrParamFmt
		}
		entries = append(entries, entry)
	}

	switch typ {
	case domain.SshHostTypeHost:
		// Host 解析记录：必须有解析内容；SSH 字段一律归一化为空（防夹带）。
		if len(entries) == 0 {
			return nil, errcode.ErrParamMiss
		}
		rec.HostEntries = entries
		return rec, nil

	case domain.SshHostTypeSSH:
		port := in.Port
		if port == 0 {
			port = SshHostDefaultPort
		}
		if port < 1 || port > 65535 {
			return nil, errcode.ErrParamFmt
		}
		user := strings.TrimSpace(in.User)
		if user == "" {
			return nil, errcode.ErrParamMiss
		}
		if !validUnixAccount(user) {
			return nil, errcode.ErrParamFmt
		}
		authType := strings.ToLower(strings.TrimSpace(in.AuthType))
		if authType != domain.SshAuthTypeKey && authType != domain.SshAuthTypePassword {
			return nil, errcode.ErrParamFmt
		}
		if current != nil && current.ID > 0 {
			rec.ID = current.ID
		}
		rec.Port = port
		rec.Username = user
		rec.AuthType = authType
		rec.HostEntries = entries
		return rec, nil
	}
	return nil, errcode.ErrParamFmt
}

// toSshHostView 领域实体 → 脱敏视图（不含私钥明文/密文）。
func toSshHostView(rec *domain.SshHost) SshHostView {
	entries := rec.HostEntries
	if entries == nil {
		entries = []string{}
	}
	return SshHostView{
		ID:          rec.ID,
		Type:        rec.Type,
		Host:        rec.Host,
		Port:        rec.Port,
		User:        rec.Username,
		AuthType:    rec.AuthType,
		HasKey:      rec.HasKey(),
		HostEntries: entries,
		Description: rec.Description,
		Version:     rec.Version,
		CreatedAt:   rec.CreatedAt,
		UpdatedAt:   rec.UpdatedAt,
	}
}

// sshHostDedupeKey 生成去重键（type|host|port）。
func sshHostDedupeKey(typ, host string, port int) string {
	return fmt.Sprintf("%s|%s|%d", typ, strings.ToLower(host), port)
}

// validHostToken 主机名/IP 白名单校验：仅允许字母数字与 . - _ : [ ]，
// 拒绝空白与 shell 元字符（DES-T-003 命令注入防护第一道）。
func validHostToken(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.', c == '-', c == '_', c == ':', c == '[', c == ']':
		default:
			return false
		}
	}
	return true
}

// validUnixAccount SSH 用户名白名单（字母数字与 . _ @ -）。
func validUnixAccount(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.', c == '_', c == '@', c == '-':
		default:
			return false
		}
	}
	return true
}

// validHostEntry Host 解析行校验：`<IP/主机> <域名...>`（至少一个映射目标）。
func validHostEntry(entry string) bool {
	if len(entry) > 256 {
		return false
	}
	fields := strings.Fields(entry)
	if len(fields) < 2 {
		return false
	}
	for _, f := range fields {
		if !validHostToken(f) {
			return false
		}
	}
	return true
}

// validateSshPrivateKey 校验 SSH 私钥可被解析（拒绝口令保护/非法内容）。
func validateSshPrivateKey(key string) error {
	signer, err := ssh.ParsePrivateKey([]byte(key))
	if err != nil {
		return fmt.Errorf("解析 SSH 私钥失败: %w", err)
	}
	if signer == nil {
		return errors.New("SSH 私钥为空")
	}
	return nil
}

// ===================== 导入文件解析 =====================

// sshHostImportRow 导入行（JSON/CSV 共用中间结构）。
type sshHostImportRow struct {
	Type        string      `json:"type"`
	Host        string      `json:"host"`
	Port        int         `json:"port"`
	User        string      `json:"user"`
	AuthType    string      `json:"auth_type"`
	Key         string      `json:"key"`
	HostEntries flexStrings `json:"host_entries"`
	Description string      `json:"description"`
}

// toInput 导入行 → 服务输入。
func (r sshHostImportRow) toInput() SshHostInput {
	return SshHostInput{
		Type:        r.Type,
		Host:        r.Host,
		Port:        r.Port,
		User:        r.User,
		AuthType:    r.AuthType,
		Key:         r.Key,
		HostEntries: []string(r.HostEntries),
		Description: r.Description,
	}
}

// flexStrings 兼容 JSON 数组与分隔符字符串（`;`/`|`）两种 host_entries 写法。
type flexStrings []string

// UnmarshalJSON 接受 ["a b"] / "a b|c d" / null。
func (f *flexStrings) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*f = nil
		return nil
	}
	var list []string
	if err := json.Unmarshal(data, &list); err == nil {
		*f = list
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*f = splitHostEntries(s)
		return nil
	}
	return fmt.Errorf("host_entries 格式非法")
}

// splitHostEntries 按 `;`/`|`/换行拆分 Host 解析行。
func splitHostEntries(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ';' || r == '|' || r == '\n' || r == '\r'
	})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// parseSshHostImportRows 按扩展名/内容特征解析 JSON 或 CSV。
func parseSshHostImportRows(filename string, data []byte) ([]sshHostImportRow, error) {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(filename)))
	switch ext {
	case ".json":
		return parseSshHostImportJSON(data)
	case ".csv":
		return parseSshHostImportCSV(data)
	}

	// 扩展名缺失/未知时按内容特征判定（JSON 以 [ 或 { 起始）。
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "{") {
		return parseSshHostImportJSON(data)
	}
	if strings.Contains(trimmed, ",") {
		return parseSshHostImportCSV(data)
	}
	return nil, ErrUploadTypeNotAllowed
}

// parseSshHostImportJSON 解析 JSON 导入（数组，或 {"items":[...]} 包装）。
func parseSshHostImportJSON(data []byte) ([]sshHostImportRow, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, errcode.ErrParamMiss
	}

	if strings.HasPrefix(trimmed, "{") {
		var wrapper struct {
			Items []sshHostImportRow `json:"items"`
		}
		if err := json.Unmarshal(data, &wrapper); err != nil {
			return nil, errcode.ErrParamFmt
		}
		return wrapper.Items, nil
	}

	var rows []sshHostImportRow
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, errcode.ErrParamFmt
	}
	return rows, nil
}

// parseSshHostImportCSV 解析 CSV 导入（首行表头，缺失列按空值处理）。
func parseSshHostImportCSV(data []byte) ([]sshHostImportRow, error) {
	reader := csv.NewReader(strings.NewReader(string(data)))
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true

	header, err := reader.Read()
	if err != nil {
		return nil, errcode.ErrParamFmt
	}
	index := make(map[string]int, len(header))
	for i, name := range header {
		key := strings.ToLower(strings.TrimSpace(name))
		if key == "username" {
			key = "user"
		}
		index[key] = i
	}
	for _, required := range []string{"type", "host"} {
		if _, ok := index[required]; !ok {
			return nil, ErrUploadTypeNotAllowed
		}
	}

	rows := make([]sshHostImportRow, 0)
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, errcode.ErrParamFmt
		}
		if len(record) == 0 || strings.TrimSpace(strings.Join(record, "")) == "" {
			continue
		}

		port := 0
		if raw := csvField(record, index, "port"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil {
				return nil, errcode.ErrParamFmt
			}
			port = parsed
		}
		rows = append(rows, sshHostImportRow{
			Type:        csvField(record, index, "type"),
			Host:        csvField(record, index, "host"),
			Port:        port,
			User:        csvField(record, index, "user"),
			AuthType:    csvField(record, index, "auth_type"),
			Key:         csvField(record, index, "key"),
			HostEntries: splitHostEntries(csvField(record, index, "host_entries")),
			Description: csvField(record, index, "description"),
		})
	}
	return rows, nil
}

// csvField 读取指定列（越界返回空串）。
func csvField(record []string, index map[string]int, key string) string {
	i, ok := index[key]
	if !ok || i >= len(record) {
		return ""
	}
	return strings.TrimSpace(record[i])
}
