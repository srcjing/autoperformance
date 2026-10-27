// Package ws WebSocket hub。
//
// 对齐 api-contract §1.1/§2.4 与 DES-S-001/DES-I-001：
//   - 握手 `WS /api/v1/ws?token=<jwt>`，服务端校验签名/有效期并加载权限并集；
//   - 订阅频道 `task:{id}`（任务进度/指标/日志）与 `logs:{type}`（用户/系统/测试日志）；
//   - 订阅按子模块权限校验（testing.task:read / log.*:read），推送前统一脱敏。
package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"auto_performance/internal/api/response"
	"auto_performance/internal/auth"
	"auto_performance/internal/errcode"
	"auto_performance/internal/logutil"
)

// 连接参数（保护服务端资源）。
const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = 25 * time.Second
	maxMessageSize = 4 * 1024
	sendBuffer     = 64
)

// Principal WS 握手时加载的鉴权快照（账号状态实时校验 + 权限并集）。
type Principal struct {
	AdminID            int64
	Permissions        map[string]bool
	MustChangePassword bool
}

// PrincipalResolver 加载管理员鉴权快照（由 service/auth 适配；测试可注入假实现）。
type PrincipalResolver func(ctx context.Context, adminID int64) (*Principal, error)

// Envelope 推送消息信封。
type Envelope struct {
	Channel string `json:"channel"`
	Event   string `json:"event"`
	Data    any    `json:"data,omitempty"`
	Time    string `json:"time"`
}

// Hub WebSocket 连接与频道管理。
type Hub struct {
	secret     string
	principals PrincipalResolver
	now        func() time.Time
	upgrader   websocket.Upgrader

	mu      sync.RWMutex
	clients map[*Client]struct{}
}

// NewHub 构造 Hub；principals 为 nil 时无法校验账号/权限（拒绝全部订阅）。
func NewHub(secret string, principals PrincipalResolver) *Hub {
	return &Hub{
		secret:     secret,
		principals: principals,
		now:        time.Now,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			// 同源校验（防 CSWSH）：浏览器请求 Origin 必须与 Host 一致；无 Origin 的非浏览器客户端放行。
			CheckOrigin: sameOrigin,
		},
		clients: make(map[*Client]struct{}),
	}
}

// Handler 返回 `/api/v1/ws` 的 gin handler（token 经 query 传递并在此鉴权）。
func (h *Hub) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := strings.TrimSpace(c.Query("token"))
		if token == "" {
			response.Error(c, errcode.ErrUnauth)
			return
		}
		claims, err := auth.ParseToken(token, h.secret)
		if err != nil {
			// 签名非法/过期统一返回未认证，避免泄露校验细节。
			response.Error(c, errcode.ErrUnauth)
			return
		}
		adminID, err := strconv.ParseInt(claims.Subject, 10, 64)
		if err != nil || adminID <= 0 {
			response.Error(c, errcode.ErrUnauth)
			return
		}

		var perms map[string]bool
		if h.principals != nil {
			principal, err := h.principals(c.Request.Context(), adminID)
			if err != nil {
				switch {
				case isErrcode(err, errcode.ErrDisabled):
					response.Error(c, errcode.ErrDisabled)
				case isErrcode(err, errcode.ErrNotFound):
					response.Error(c, errcode.ErrUnauth)
				default:
					response.Error(c, errcode.ErrDependency)
				}
				return
			}
			if principal == nil {
				response.Error(c, errcode.ErrUnauth)
				return
			}
			// 首登强制改密期间禁止建立实时通道（与业务接口口径一致）。
			if principal.MustChangePassword {
				response.Error(c, errcode.ErrMustPwd)
				return
			}
			perms = principal.Permissions
		}

		conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			// Upgrade 已写入错误响应，无需重复输出。
			return
		}
		client := newClient(h, conn, adminID, perms)
		h.register(client)
		go client.writePump()
		go client.readPump()
	}
}

// Publish 向频道推送事件（推送前统一脱敏，DES-I-001）。
func (h *Hub) Publish(channel, event string, payload any) {
	if h == nil {
		return
	}
	env := Envelope{Channel: channel, Event: event, Data: payload, Time: h.now().UTC().Format(time.RFC3339Nano)}
	// 推送前统一脱敏：只替换 JSON 字符串值内容，避免破坏 JSON 结构（DES-I-001）。
	env.Data = sanitizeValue(env.Data)
	raw, err := json.Marshal(env)
	if err != nil {
		return
	}
	msg := raw

	h.mu.RLock()
	targets := make([]*Client, 0, len(h.clients))
	for c := range h.clients {
		if c.subscribed(channel) {
			targets = append(targets, c)
		}
	}
	h.mu.RUnlock()

	for _, c := range targets {
		c.enqueue(msg)
	}
}

// ClientCount 当前连接数（健康检查/测试用）。
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Close 关闭全部连接（优雅退出）。
func (h *Hub) Close() {
	h.mu.Lock()
	clients := make([]*Client, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.clients = make(map[*Client]struct{})
	h.mu.Unlock()

	for _, c := range clients {
		c.close()
	}
}

// register 登记连接。
func (h *Hub) register(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[c] = struct{}{}
}

// unregister 注销连接。
func (h *Hub) unregister(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		c.close()
	}
}

// channelPermission 频道 → 所需权限点（task:{id} 需 testing.task:read；logs:{type} 需 log.{type}:read）。
func channelPermission(channel string) (module, perm string, ok bool) {
	if strings.HasPrefix(channel, "task:") {
		id := strings.TrimPrefix(channel, "task:")
		if _, err := strconv.ParseInt(id, 10, 64); err != nil {
			return "", "", false
		}
		return "testing.task", "read", true
	}
	switch channel {
	case "logs:user":
		return "log.user", "read", true
	case "logs:system":
		return "log.system", "read", true
	case "logs:test":
		return "log.test", "read", true
	default:
		return "", "", false
	}
}

// authorize 校验频道订阅权限。
func authorize(perms map[string]bool, channel string) error {
	module, perm, ok := channelPermission(channel)
	if !ok {
		return fmt.Errorf("未知频道: %s", channel)
	}
	if !auth.HasPerm(perms, module, perm) {
		return fmt.Errorf("无权订阅频道: %s", channel)
	}
	return nil
}

// sanitizeValue 递归脱敏 JSON 载荷中的字符串值（Authorization/password/私钥特征）。
//
// 必须在序列化前按值脱敏，而不是对 JSON 文本做正则替换：后者会吞掉引号/花括号导致报文损坏。
func sanitizeValue(v any) any {
	return sanitizeValueDepth(v, 0)
}

// sanitizeMaxDepth 递归深度上限：防止环形引用/超深嵌套导致栈溢出（曾因此崩溃）。
const sanitizeMaxDepth = 32

func sanitizeValueDepth(v any, depth int) any {
	if depth > sanitizeMaxDepth {
		return nil
	}
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		return logutil.SanitizeLog(t)
	case bool, json.Number,
		float32, float64,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		// 标量直接返回（关键：不得走 JSON 往返，否则 float64/bool 会无限递归）。
		return v
	case map[string]string:
		out := make(map[string]string, len(t))
		for k, val := range t {
			out[k] = logutil.SanitizeLog(val)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = sanitizeValueDepth(val, depth+1)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i := range t {
			out[i] = sanitizeValueDepth(t[i], depth+1)
		}
		return out
	case []string:
		out := make([]string, len(t))
		for i := range t {
			out[i] = logutil.SanitizeLog(t[i])
		}
		return out
	default:
		// 结构体等复合类型：JSON 往返后递归（带深度限制，双保险）。
		raw, err := json.Marshal(v)
		if err != nil {
			return nil
		}
		var generic any
		if err := json.Unmarshal(raw, &generic); err != nil {
			return nil
		}
		return sanitizeValueDepth(generic, depth+1)
	}
}

// sameOrigin 校验握手 Origin 与请求 Host 一致（同源部署）。
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

// isErrcode 判断错误是否为指定业务码（兼容 %w 包装）。
func isErrcode(err error, target *errcode.Error) bool {
	return errors.Is(err, target)
}
