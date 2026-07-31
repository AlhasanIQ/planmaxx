package review

import (
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	presencePongWait   = 45 * time.Second
	presencePingPeriod = 25 * time.Second
	presenceWriteWait  = 10 * time.Second
)

type browserPresence struct {
	mu       sync.Mutex
	timeout  time.Duration
	conns    map[*websocket.Conn]struct{}
	closed   bool
	timer    *time.Timer
	timerID  uint64
	onOrphan func()
}

func newBrowserPresence(onOrphan func()) *browserPresence {
	return &browserPresence{
		conns:    make(map[*websocket.Conn]struct{}),
		onOrphan: onOrphan,
	}
}

func (p *browserPresence) SetTimeout(timeout time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.timeout = timeout
	p.stopTimerLocked()
	p.startTimerLocked()
}

func (p *browserPresence) Serve(conn *websocket.Conn) {
	if !p.add(conn) {
		_ = conn.Close()
		return
	}
	defer func() {
		p.remove(conn)
		_ = conn.Close()
	}()

	conn.SetReadLimit(1024)
	_ = conn.SetReadDeadline(time.Now().Add(presencePongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(presencePongWait))
	})

	pingDone := make(chan struct{})
	stopPing := make(chan struct{})
	go func() {
		defer close(pingDone)
		ticker := time.NewTicker(presencePingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-stopPing:
				return
			case <-ticker.C:
				_ = conn.SetWriteDeadline(time.Now().Add(presenceWriteWait))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			}
		}
	}()

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
	close(stopPing)
	_ = conn.Close()
	<-pingDone
}

func (p *browserPresence) add(conn *websocket.Conn) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return false
	}
	p.conns[conn] = struct{}{}
	p.stopTimerLocked()
	return true
}

func (p *browserPresence) remove(conn *websocket.Conn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.conns[conn]; !ok {
		return
	}
	delete(p.conns, conn)
	p.startTimerLocked()
}

func (p *browserPresence) startTimerLocked() {
	if p.closed || len(p.conns) != 0 || p.timeout <= 0 {
		return
	}
	p.timerID++
	timerID := p.timerID
	p.timer = time.AfterFunc(p.timeout, func() { p.expire(timerID) })
}

func (p *browserPresence) expire(timerID uint64) {
	p.mu.Lock()
	if p.closed || timerID != p.timerID || len(p.conns) != 0 {
		p.mu.Unlock()
		return
	}
	p.closed = true
	p.timer = nil
	onOrphan := p.onOrphan
	p.mu.Unlock()

	if onOrphan != nil {
		onOrphan()
	}
}

func (p *browserPresence) stopTimerLocked() {
	p.timerID++
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
}

func (p *browserPresence) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	p.stopTimerLocked()
	conns := make([]*websocket.Conn, 0, len(p.conns))
	for conn := range p.conns {
		conns = append(conns, conn)
	}
	p.mu.Unlock()

	for _, conn := range conns {
		_ = conn.Close()
	}
}

var presenceUpgrader = websocket.Upgrader{}

func (s *Server) handlePresence(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	s.mu.Lock()
	finished := s.finished
	s.mu.Unlock()
	if finished {
		writeError(w, http.StatusConflict, "review already completed")
		return
	}

	conn, err := presenceUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	s.presence.Serve(conn)
}
