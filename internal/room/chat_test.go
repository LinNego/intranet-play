package room

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"intranet-play/internal/netx"
	"intranet-play/internal/protocol"
)

// 聊天不做服务端回声：房主只把消息转发给别人，发送方要靠本地记一条。
// 21 点与炸金花的客户端一度漏了这一步，表现为"加入者看不到自己说的话"。
// 这里用真实连接驱动客户端那一侧，把这条行为钉住。

// newConnPair 建一对已连上的 netx.Conn：前者当"客户端"，后者当"房主端"。
func newConnPair(t *testing.T) (client, peer *netx.Conn) {
	t.Helper()
	ln, err := netx.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	defer ln.Close()

	type result struct {
		conn *netx.Conn
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		c, err := netx.Accept(ln)
		ch <- result{c, err}
	}()

	client, err = netx.Connect(ln.Addr().String())
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	got := <-ch
	if got.err != nil {
		t.Fatalf("accept 失败: %v", got.err)
	}
	return client, got.conn
}

// expectChat 断言对端确实收到了这条聊天。
func expectChat(t *testing.T, peer *netx.Conn, want string) {
	t.Helper()
	select {
	case env, ok := <-peer.Inbox():
		if !ok {
			t.Fatal("对端连接已关闭")
		}
		if env.Type != protocol.TypeChat {
			t.Fatalf("对端收到 %s, 期望 %s", env.Type, protocol.TypeChat)
		}
		var p protocol.ChatPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			t.Fatalf("载荷解析失败: %v", err)
		}
		if p.Message != want {
			t.Fatalf("对端收到的内容 = %q, 期望 %q", p.Message, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("对端没有收到聊天")
	}
}

// assertOwnChatLogged 断言发送方自己的面板里也有一条。
func assertOwnChatLogged(t *testing.T, log []string, want string) {
	t.Helper()
	joined := strings.Join(log, "\n")
	if !strings.Contains(joined, "我: "+want) {
		t.Fatalf("自己发的话没显示在面板里（期望含 %q）:\n%s", "我: "+want, joined)
	}
}

func TestZjhClientSeesOwnChat(t *testing.T) {
	client, peer := newConnPair(t)
	defer client.Close()
	defer peer.Close()

	r := newZjhRoom("9988", 100, 1, 20, 6, 4)
	r.you = 1
	if done := r.handleClientLocal("chat 摸鱼中", client, "Bob"); done {
		t.Fatal("chat 不该结束会话")
	}
	assertOwnChatLogged(t, r.log, "摸鱼中")
	expectChat(t, peer, "摸鱼中")
}

func TestBjClientSeesOwnChat(t *testing.T) {
	client, peer := newConnPair(t)
	defer client.Close()
	defer peer.Close()

	r := newBjRoom("9988", 100, 4, "auto")
	r.you = 1
	if done := r.handleClientLocal("chat 手气不错", client, "Bob"); done {
		t.Fatal("chat 不该结束会话")
	}
	assertOwnChatLogged(t, r.log, "手气不错")
	expectChat(t, peer, "手气不错")
}

func TestDdzClientSeesOwnChat(t *testing.T) {
	client, peer := newConnPair(t)
	defer client.Close()
	defer peer.Close()

	r := newDdzRoom("9988", 1, 10)
	r.you = 1
	if done := r.handleClientLocal("chat 该你出牌了", client, "Bob"); done {
		t.Fatal("chat 不该结束会话")
	}
	assertOwnChatLogged(t, r.log, "该你出牌了")
	expectChat(t, peer, "该你出牌了")
}
