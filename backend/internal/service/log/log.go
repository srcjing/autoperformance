// Package log 日志业务服务（用户/系统/测试日志 + 审计 HMAC 链）。
//
// 本文件实现审计日志追加与链式签名（INV-8 / DES-T-006 / CODE-T-006）：
//   - hash = HMAC-SHA256(prevHash || entryJSON)，逐条链接，单条篡改可被检测；
//   - 写库前统一脱敏（DES-I-001），禁止明文口令/认证头/私钥落库；
//   - 应用层只追加，无 UPDATE/DELETE 入口（DB 触发器兜底）。
package log

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"auto_performance/internal/domain"
	"auto_performance/internal/logutil"
)

// AuditEvent 待追加的审计事件（5W1H：Who/When/Where/What/How/Result）。
type AuditEvent struct {
	AdminID      *int64 // 操作人（未认证请求为 nil）
	Action       string // 动作枚举：login/logout/create/update/delete/start/stop...
	ResourceType string // 资源类型：admin/environment/task/report...
	ResourceID   string // 资源 ID（无则空）
	Detail       string // 细节（写库前自动脱敏）
	IP           string // 来源 IP
	Result       string // ok | fail
}

// AuditStore 审计日志仓储能力（由 repository.PGRepository 实现）。
// 契约：只提供追加与读取末条 hash，不提供 UPDATE/DELETE（INV-8）。
type AuditStore interface {
	// LastAuditHash 返回最近一条用户日志的 hash；无记录时返回 (nil, nil)。
	LastAuditHash(ctx context.Context) ([]byte, error)
	// AppendUserLog 追加一条用户日志（user_log）。
	AppendUserLog(ctx context.Context, entry *domain.AuditLog) error
}

// Service 日志服务（审计链写入 + 四类日志查询 + 请求详情/日志设置 + 系统日志事件）。
type Service struct {
	store AuditStore
	// query 日志查询/系统日志仓储（DB 不可用时为 nil，查询降级为依赖异常）。
	query QueryStore
	// request 请求详情与日志设置仓储（DB 不可用时为 nil，写入静默跳过、查询降级为依赖异常）。
	request RequestStore

	// mu 串行化本实例内的追加，避免并发请求读到同一个 prevHash 造成链分叉。
	// 跨实例（分布式部署）追加需由 DB advisory lock 兜底，见 repository.AppendUserLog。
	mu sync.Mutex

	// now 时钟（测试可注入；nil 时使用 time.Now）。
	now func() time.Time
}

// NewService 构造日志服务；store 为 nil 时不落库（降级为仅本地打印）。
func NewService(store AuditStore) *Service {
	return &Service{store: store, now: time.Now}
}

// chainPayload 参与签名的规范化载荷。
//
// 字段顺序固定（JSON 序列化按结构体声明顺序输出），保证同一事件在任意时刻可复算出同一 hash。
// 不含 DB 自增 id 与 hash 自身，避免「先算 hash 再落库」的循环依赖。
type chainPayload struct {
	AdminID      *int64 `json:"admin_id"`
	Action       string `json:"action"`
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
	Detail       string `json:"detail"`
	IP           string `json:"ip"`
	Result       string `json:"result"`
	CreatedAt    string `json:"created_at"`
}

// ChainHash 计算审计链 hash：HMAC-SHA256(prevHash || entryJSON)。
func ChainHash(prevHash []byte, event AuditEvent, createdAt time.Time, hmacKey []byte) ([]byte, error) {
	if len(hmacKey) == 0 {
		return nil, fmt.Errorf("审计链 HMAC 密钥为空")
	}
	payload, err := json.Marshal(chainPayload{
		AdminID:      event.AdminID,
		Action:       event.Action,
		ResourceType: event.ResourceType,
		ResourceID:   event.ResourceID,
		Detail:       event.Detail,
		IP:           event.IP,
		Result:       event.Result,
		CreatedAt:    createdAt.UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return nil, fmt.Errorf("序列化审计事件失败: %w", err)
	}

	mac := hmac.New(sha256.New, hmacKey)
	mac.Write(prevHash)
	mac.Write(payload)
	return mac.Sum(nil), nil
}

// AppendAudit 追加一条审计日志并返回其 hash。
//
// prevHash 为链上一条的 hash；传 nil 时由服务读取当前链尾（首条记录亦为 nil）。
// 返回的 hash 可回传给下一次调用，便于调用方自行串联（例如批量写入）。
func (s *Service) AppendAudit(ctx context.Context, prevHash []byte, event AuditEvent, hmacKey []byte) ([]byte, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("审计仓储未初始化")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if prevHash == nil {
		last, err := s.store.LastAuditHash(ctx)
		if err != nil {
			return nil, fmt.Errorf("读取审计链尾失败: %w", err)
		}
		prevHash = last
	}

	now := s.nowTime()
	entry := &domain.AuditLog{
		AdminID:      event.AdminID,
		Action:       truncate(logutil.SanitizeLog(event.Action), 64),
		ResourceType: truncate(logutil.SanitizeLog(event.ResourceType), 64),
		ResourceID:   truncate(logutil.SanitizeLog(event.ResourceID), 64),
		Detail:       logutil.SanitizeLog(event.Detail),
		IP:           truncate(logutil.SanitizeLog(event.IP), 64),
		Result:       truncate(event.Result, 16),
		PrevHash:     prevHash,
		CreatedAt:    now,
	}
	if entry.Result == "" {
		entry.Result = domain.AuditResultOK
	}

	hash, err := ChainHash(prevHash, AuditEvent{
		AdminID:      entry.AdminID,
		Action:       entry.Action,
		ResourceType: entry.ResourceType,
		ResourceID:   entry.ResourceID,
		Detail:       entry.Detail,
		IP:           entry.IP,
		Result:       entry.Result,
	}, now, hmacKey)
	if err != nil {
		return nil, err
	}
	entry.Hash = hash

	if err := s.store.AppendUserLog(ctx, entry); err != nil {
		return nil, fmt.Errorf("写入审计日志失败: %w", err)
	}
	return hash, nil
}

// VerifyAuditChain 校验一段按写入顺序排列的审计日志链。
// 首条以自身 prev_hash 为链头（无法回溯更早的历史）；任意条目内容或链接被改写即返回错误。
func VerifyAuditChain(entries []domain.AuditLog, hmacKey []byte) error {
	var prev []byte
	for i := range entries {
		e := &entries[i]
		if i == 0 {
			prev = e.PrevHash
		}
		if !hmac.Equal(e.PrevHash, prev) {
			return fmt.Errorf("审计链在 id=%d 处断裂：prev_hash 不匹配", e.ID)
		}
		want, err := ChainHash(prev, auditEventOf(e), e.CreatedAt, hmacKey)
		if err != nil {
			return err
		}
		if !hmac.Equal(want, e.Hash) {
			return fmt.Errorf("审计链在 id=%d 处校验失败：hash 不匹配（内容可能被篡改）", e.ID)
		}
		prev = e.Hash
	}
	return nil
}

// DeriveAuditKey 解析审计链 HMAC 密钥（对齐 DES-I-008：密钥不硬编码、来源可轮换）。
//
// 优先级：显式配置 security.audit_hmac_key > security.secret_key_env 指向的环境变量 > JWT secret 派生。
// 返回值固定 32 字节，可直接作为 HMAC-SHA256 密钥。
func DeriveAuditKey(explicit, envSecret, fallbackSecret string) []byte {
	var material string
	switch {
	case explicit != "":
		material = "explicit|" + explicit
	case envSecret != "":
		material = "env|" + envSecret
	default:
		material = "derived|" + fallbackSecret
	}
	sum := sha256.Sum256([]byte("auto_performance/audit-hmac/v1|" + material))
	return sum[:]
}

// Fingerprint 返回密钥指纹（前 4 字节 hex），用于启动日志中确认密钥来源一致而不泄露密钥本身。
func Fingerprint(key []byte) string {
	sum := sha256.Sum256(key)
	return hex.EncodeToString(sum[:4])
}

func auditEventOf(e *domain.AuditLog) AuditEvent {
	return AuditEvent{
		AdminID:      e.AdminID,
		Action:       e.Action,
		ResourceType: e.ResourceType,
		ResourceID:   e.ResourceID,
		Detail:       e.Detail,
		IP:           e.IP,
		Result:       e.Result,
	}
}

// truncate 按字符截断，避免超长值触发 DB 列长度错误（列宽见 migrations/000001）。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
