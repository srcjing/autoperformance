package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"auto_performance/internal/auth"
	"auto_performance/internal/errcode"
)

const testSecret = "ws-test-secret"

// newTestServer 构造带 WS 路由的测试服务（权限由 fake resolver 提供）。
func newTestServer(t *testing.T) (*Hub, *httptest.Server) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	hub := NewHub(testSecret, func(_ context.Context, adminID int64) (*Principal, error) {
		if adminID != 7 {
			return nil, errcode.ErrNotFound
		}
		return &Principal{AdminID: adminID, Permissions: map[string]bool{"testing.task:read": true, "log.test:read": true}}, nil
	})
	r := gin.New()
	r.GET("/api/v1/ws", hub.Handler())
	srv := httptest.NewServer(r)
	t.Cleanup(func() {
		hub.Close()
		srv.Close()
	})
	return hub, srv
}

// TestWSRejectsNoToken 无 token / 伪造 token 一律拒绝握手（DES-S-001 / TEST-S-001）。
func TestWSRejectsNoToken(t *testing.T) {
	_, srv := newTestServer(t)

	for _, path := range []string{
		"/api/v1/ws",
		"/api/v1/ws?token=",
		"/api/v1/ws?token=not-a-jwt",
	} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		body := make([]byte, 256)
		n, _ := resp.Body.Read(body)
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s status = %d, want 401", path, resp.StatusCode)
		}
		var parsed struct {
			Code int `json:"code"`
		}
		if err := json.Unmarshal(body[:n], &parsed); err != nil || parsed.Code != errcode.ErrUnauth.Code {
			t.Fatalf("%s code = %d, want %d (body=%s)", path, parsed.Code, errcode.ErrUnauth.Code, string(body[:n]))
		}
	}
}

// TestWSSubscribeAndSanitize 合法 token 可订阅 task:{id}，推送前剥离认证头（DES-I-001）。
func TestWSSubscribeAndSanitize(t *testing.T) {
	hub, srv := newTestServer(t)
	token, err := auth.GenerateToken("7", "alice", testSecret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/v1/ws?token=" + token
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("握手失败: %v", err)
	}
	defer func() { _ = conn.Close() }()

	if err := conn.WriteJSON(map[string]string{"action": "subscribe", "channel": "task:12"}); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var ack Envelope
	if err := conn.ReadJSON(&ack); err != nil {
		t.Fatalf("读取订阅回执失败: %v", err)
	}
	if ack.Event != "subscribed" {
		t.Fatalf("订阅回执异常: %+v", ack)
	}

	hub.Publish("task:12", "log", map[string]string{
		"message": "Authorization: Bearer super-secret-token password=123456",
	})
	var msg Envelope
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("读取推送失败: %v", err)
	}
	raw := strings.ToLower(msg.Data.(map[string]any)["message"].(string))
	if strings.Contains(raw, "super-secret-token") || strings.Contains(raw, "123456") {
		t.Fatalf("推送内容未脱敏: %s", raw)
	}
	if !strings.Contains(raw, "authorization") {
		t.Fatalf("脱敏应保留键名: %s", raw)
	}
}

// TestWSSubscribePermissionDenied 无对应权限点拒绝订阅（服务端最终鉴权）。
func TestWSSubscribePermissionDenied(t *testing.T) {
	hub, srv := newTestServer(t)
	token, err := auth.GenerateToken("7", "alice", testSecret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/v1/ws?token=" + token
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	// log.user 未授权 → error 事件。
	if err := conn.WriteJSON(map[string]string{"action": "subscribe", "channel": "logs:user"}); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var ack Envelope
	if err := conn.ReadJSON(&ack); err != nil {
		t.Fatal(err)
	}
	if ack.Event != "error" {
		t.Fatalf("未授权频道应返回 error, got %+v", ack)
	}

	// 未知频道同样拒绝。
	if err := conn.WriteJSON(map[string]string{"action": "subscribe", "channel": "task:abc"}); err != nil {
		t.Fatal(err)
	}
	if err := conn.ReadJSON(&ack); err != nil {
		t.Fatal(err)
	}
	if ack.Event != "error" {
		t.Fatalf("非法频道应返回 error, got %+v", ack)
	}

	_ = hub.ClientCount()
}

// TestWSRejectsDisabledAccount 账号停用/不存在时拒绝握手。
func TestWSRejectsDisabledAccount(t *testing.T) {
	_, srv := newTestServer(t)
	token, err := auth.GenerateToken("99", "ghost", testSecret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(srv.URL + "/api/v1/ws?token=" + token)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

// TestWSRejectsMustChangePassword 首登未改密账号禁止建立实时通道（10103）。
func TestWSRejectsMustChangePassword(t *testing.T) {
	gin.SetMode(gin.TestMode)
	hub := NewHub(testSecret, func(_ context.Context, adminID int64) (*Principal, error) {
		return &Principal{AdminID: adminID, Permissions: map[string]bool{"testing.task:read": true}, MustChangePassword: true}, nil
	})
	defer hub.Close()
	r := gin.New()
	r.GET("/api/v1/ws", hub.Handler())
	srv := httptest.NewServer(r)
	defer srv.Close()

	token, err := auth.GenerateToken("7", "alice", testSecret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(srv.URL + "/api/v1/ws?token=" + token)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403（需修改密码）", resp.StatusCode)
	}
}
