package ws

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// maxSubscriptions 单连接订阅频道上限（保护服务端推送扇出）。
const maxSubscriptions = 20

// request 客户端指令：subscribe / unsubscribe / ping。
type request struct {
	Action  string `json:"action"`
	Channel string `json:"channel"`
}

// Client 单条 WebSocket 连接（读/写协程 + 订阅集合）。
type Client struct {
	hub     *Hub
	conn    *websocket.Conn
	adminID int64
	perms   map[string]bool
	send    chan []byte

	mu     sync.RWMutex
	subs   map[string]bool
	closed bool

	closeOnce sync.Once
}

// newClient 构造连接。
func newClient(hub *Hub, conn *websocket.Conn, adminID int64, perms map[string]bool) *Client {
	return &Client{
		hub:     hub,
		conn:    conn,
		adminID: adminID,
		perms:   perms,
		send:    make(chan []byte, sendBuffer),
		subs:    make(map[string]bool, 4),
	}
}

// subscribed 判断是否订阅了频道。
func (c *Client) subscribed(channel string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return !c.closed && c.subs[channel]
}

// enqueue 非阻塞入队（慢消费者直接丢弃，避免拖垮推送路径）。
func (c *Client) enqueue(msg []byte) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.closed {
		return
	}
	select {
	case c.send <- msg:
	default:
	}
}

// readPump 处理订阅指令；连接断开即清理。
func (c *Client) readPump() {
	defer c.hub.unregister(c)

	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		var req request
		if err := json.Unmarshal(raw, &req); err != nil {
			c.reply("error", map[string]any{"message": "指令格式非法"})
			continue
		}
		switch req.Action {
		case "subscribe":
			c.subscribe(req.Channel)
		case "unsubscribe":
			c.unsubscribe(req.Channel)
		case "ping":
			c.reply("pong", map[string]any{"admin_id": c.adminID})
		default:
			c.reply("error", map[string]any{"message": "不支持的指令"})
		}
	}
}

// subscribe 订阅频道（服务端按权限点校验，前端隐藏不可绕过）。
func (c *Client) subscribe(channel string) {
	if err := authorize(c.perms, channel); err != nil {
		c.reply("error", map[string]any{"channel": channel, "message": err.Error()})
		return
	}
	c.mu.Lock()
	if len(c.subs) >= maxSubscriptions && !c.subs[channel] {
		c.mu.Unlock()
		c.reply("error", map[string]any{"channel": channel, "message": "订阅频道数超限"})
		return
	}
	c.subs[channel] = true
	c.mu.Unlock()
	c.reply("subscribed", map[string]any{"channel": channel})
}

// unsubscribe 取消订阅。
func (c *Client) unsubscribe(channel string) {
	c.mu.Lock()
	delete(c.subs, channel)
	c.mu.Unlock()
	c.reply("unsubscribed", map[string]any{"channel": channel})
}

// reply 直接向本连接回执系统事件（同样经脱敏）。
func (c *Client) reply(event string, data any) {
	env := Envelope{Channel: "system", Event: event, Data: sanitizeValue(data), Time: c.hub.now().UTC().Format(time.RFC3339)}
	raw, err := json.Marshal(env)
	if err != nil {
		return
	}
	c.enqueue(raw)
}

// writePump 推送消息 + 心跳（Ping）。
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.hub.unregister(c)
	}()

	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				return
			}
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// close 关闭连接（幂等）。
//
// 不关闭 send channel：写协程可能阻塞在 select 上，直接关闭会触发 panic；
// 连接关闭后 ping 失败使写协程自行退出。
func (c *Client) close() {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
		_ = c.conn.Close()
	})
}
