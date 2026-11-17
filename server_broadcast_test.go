package wslt

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func waitServerCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for !condition() {
		select {
		case <-deadline.C:
			t.Fatal("server condition timed out")
		case <-ticker.C:
		}
	}
}

func closeTestConnection(c *Connection, start <-chan struct{}, finished chan<- struct{}) {
	<-start
	c.Close()
	finished <- struct{}{}
}

func sendTestConnection(c *Connection, start <-chan struct{}, finished chan<- struct{}) {
	<-start
	_ = c.SendWsMessage(&WsMessage{Type: websocket.TextMessage})
	finished <- struct{}{}
}

func broadcastTestConnection(w *WSocket, start <-chan struct{}, finished chan<- struct{}) {
	<-start
	_ = w.SendToAll("lotteryDraw", 1)
	finished <- struct{}{}
}

func TestServerBroadcastConcurrentClose(t *testing.T) {
	w := NewWSocket(WithConnectionQueueSize(1))
	defer w.Close()
	// Exercise the real hub with 1000 send/broadcast/close interleavings.
	// No writer drains these queues, so fanout also exercises slow-peer eviction.
	for i := 0; i < 1000; i++ {
		c := newConnection(nil, w)
		w.register <- c
		start := make(chan struct{})
		finished := make(chan struct{}, 3)
		go closeTestConnection(c, start, finished)
		go sendTestConnection(c, start, finished)
		go broadcastTestConnection(w, start, finished)
		close(start)
		timeout := time.NewTimer(3 * time.Second)
		for j := 0; j < 3; j++ {
			select {
			case <-finished:
			case <-timeout.C:
				t.Fatalf("deadlock at iteration %d", i)
			}
		}
		timeout.Stop()
	}
	waitServerCondition(t, func() bool { return w.ConnectionCount() == 0 })
}

func TestServerFullConnectionQueue(t *testing.T) {
	w := NewWSocket(WithConnectionQueueSize(1))
	defer w.Close()
	c := newConnection(nil, w)
	c.sent <- &WsMessage{}
	started := time.Now()
	if err := c.SendWsMessage(&WsMessage{}); err == nil {
		t.Fatal("full queue accepted message")
	}
	if time.Since(started) > time.Second {
		t.Fatal("full queue blocked")
	}
	start := make(chan struct{})
	finished := make(chan struct{}, 1)
	go sendTestConnection(c, start, finished)
	close(start)
	c.Close()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("close did not release sender")
	}
	if err := c.SendWsMessage(&WsMessage{}); err == nil {
		t.Fatal("closed peer accepted message")
	}
}

// stallTestHub blocks only the caller-supplied iteration callback, not a socket.
func stallTestHub(t *testing.T, w *WSocket) {
	t.Helper()
	c := newConnection(nil, w)
	w.register <- c
	entered := make(chan struct{})
	release := make(chan struct{})
	w.IterationConnections(func(string, *Connection) bool {
		close(entered)
		<-release
		return false
	})
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("hub callback did not start")
	}
	t.Cleanup(func() { close(release) })
}

func TestServerFullBroadcastQueue(t *testing.T) {
	w := NewWSocket(WithBroadcastQueueSize(1), WithBroadcastTimeout(25*time.Millisecond))
	defer w.Close()
	stallTestHub(t, w)
	if err := w.SendToAll("first", 1); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := w.SendToAll("full", 2); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout: %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("SendToAll blocked")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	started = time.Now()
	if err := w.SendToAllContext(ctx, "full", 3); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("context timeout: %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("SendToAllContext blocked")
	}
	canceled, stop := context.WithCancel(context.Background())
	stop()
	if err := w.SendToAllContext(canceled, "canceled", 4); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	// Closing must work even while the hub is stuck in a user callback.
	w.Close()
	if err := w.SendToAll("closed", 5); err == nil {
		t.Fatal("closed hub accepted broadcast")
	}
}

func TestServerSlowPeerDoesNotBlockFanout(t *testing.T) {
	w := NewWSocket(WithConnectionQueueSize(1))
	defer w.Close()
	slow := newConnection(nil, w)
	slow.sent <- &WsMessage{}
	healthy := newConnection(nil, w)
	w.register <- slow
	w.register <- healthy
	if err := w.SendToAll("lotteryDraw", 42); err != nil {
		t.Fatal(err)
	}
	select {
	case <-healthy.sent:
	case <-time.After(time.Second):
		t.Fatal("slow peer blocked healthy peer")
	}
	waitServerCondition(t, func() bool { return slow.IsClosed() && w.ConnectionCount() == 1 })
	if err := slow.SendWsMessage(&WsMessage{}); err == nil {
		t.Fatal("slow peer was not disconnected")
	}
}

func TestServerLocalDeliveryAndUnregister(t *testing.T) {
	connected := make(chan *Connection, 1)
	w := NewWSocket(WithAfterConnectFunc(func(c *Connection) { connected <- c }))
	defer w.Close()
	srv := httptest.NewServer(w)
	defer srv.Close()
	peer, _, err := websocket.DefaultDialer.Dial(wsURLFromHTTP(srv.URL), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	c := <-connected
	waitServerCondition(t, func() bool { return w.ConnectionCount() == 1 })
	if err = w.SendToAll("lotteryDraw", 42); err != nil {
		t.Fatal(err)
	}
	_ = peer.SetReadDeadline(time.Now().Add(time.Second))
	_, payload, err := peer.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	message, decodeErr := DecodeBiMessage(payload)
	if decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if message.Type != "lotteryDraw" {
		t.Fatalf("unexpected broadcast: %s (%v)", payload, decodeErr)
	}
	_ = peer.Close()
	waitServerCondition(t, func() bool { return c.IsClosed() && w.ConnectionCount() == 0 })
}

func writeTestPing(c *Connection, result chan<- error) { result <- c.writePing() }

func TestServerPingWithFullSentQueue(t *testing.T) {
	connected := make(chan *websocket.Conn, 1)
	srv := newTestWsServer(t, func(c *websocket.Conn) { connected <- c })
	defer srv.Close()
	peer, _, err := websocket.DefaultDialer.Dial(wsURLFromHTTP(srv.URL), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	serverConn := <-connected
	defer serverConn.Close()
	w := NewWSocket(WithConnectionQueueSize(1))
	defer w.Close()
	c := newConnection(serverConn, w)
	c.sent <- &WsMessage{}
	result := make(chan error, 1)
	go writeTestPing(c, result)
	select {
	case err = <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("ping blocked on its own full queue")
	}
	if len(c.sent) != 1 {
		t.Fatal("ping consumed or replaced queued data")
	}
	receivedPing := errors.New("received ping")
	peer.SetPingHandler(func(string) error { return receivedPing })
	_ = peer.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err = peer.ReadMessage(); !errors.Is(err, receivedPing) {
		t.Fatalf("ping not received: %v", err)
	}
}

func TestServerDefaultsAndClose(t *testing.T) {
	w := NewWSocket()
	if cap(w.broadcast) != 1024 || cap(newConnection(nil, w).sent) != 256 || w.broadcastTimeout != 2*time.Second {
		t.Fatal("unexpected defaults")
	}
	var workers sync.WaitGroup
	for i := 0; i < 10; i++ {
		workers.Add(1)
		go closeTestHub(w, &workers)
	}
	workers.Wait()
	if err := w.SendToAllContext(context.Background(), "closed", 1); err == nil {
		t.Fatal("closed hub accepted broadcast")
	}
}

func closeTestHub(w *WSocket, workers *sync.WaitGroup) { defer workers.Done(); w.Close() }

func sendTestContext(w *WSocket, ctx context.Context, result chan<- error) {
	result <- w.SendToAllContext(ctx, "waiting", 1)
}

func TestServerBlockedBroadcastReleased(t *testing.T) {
	w := NewWSocket(WithBroadcastQueueSize(1))
	defer w.Close()
	stallTestHub(t, w)
	if err := w.SendToAll("first", 1); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go sendTestContext(w, ctx, result)
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not release broadcast sender")
	}
	go sendTestContext(w, context.Background(), result)
	w.Close()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("closed hub accepted waiting broadcast")
		}
	case <-time.After(time.Second):
		t.Fatal("hub close did not release broadcast sender")
	}
}

func TestServerClosedBeforeRegistration(t *testing.T) {
	connected := make(chan *Connection, 1)
	w := NewWSocket(WithAfterConnectFunc(func(c *Connection) {
		c.Close()
		connected <- c
	}))
	defer w.Close()
	srv := httptest.NewServer(w)
	defer srv.Close()
	peer, _, err := websocket.DefaultDialer.Dial(wsURLFromHTTP(srv.URL), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	c := <-connected
	_ = peer.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err = peer.ReadMessage(); err == nil {
		t.Fatal("closed peer remained connected")
	}
	if !c.IsClosed() {
		t.Fatal("connection not closed")
	}
	// A second registration acts as a hub barrier: the first one must have
	// completed (or been skipped) before this count is observed.
	barrier := newConnection(nil, w)
	w.register <- barrier
	waitServerCondition(t, func() bool { return w.ConnectionCount() == 1 })
}
