package test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

func (s *interactiveServer) appendLog(line string) {
	s.mu.Lock()
	s.logs = append(s.logs, line)
	subs := append([]chan interactiveEvent(nil), s.subscribers...)
	s.mu.Unlock()

	for _, sub := range subs {
		select {
		case sub <- interactiveEvent{Type: "log", Line: line}:
		default:
		}
	}
}

func (s *interactiveServer) broadcast(ev interactiveEvent) {
	s.mu.Lock()
	subs := append([]chan interactiveEvent(nil), s.subscribers...)
	s.mu.Unlock()

	for _, sub := range subs {
		select {
		case sub <- ev:
		default:
		}
	}
}

func (s *interactiveServer) removeSubscriber(target chan interactiveEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, sub := range s.subscribers {
		if sub == target {
			s.subscribers = append(s.subscribers[:i], s.subscribers[i+1:]...)
			return
		}
	}
}

func writeSSE(w io.Writer, flusher http.Flusher, ev interactiveEvent) {
	payload, err := json.Marshal(ev)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", payload)
	flusher.Flush()
}

type logTap struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	onLine func(string)
}

func (t *logTap) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	n, err := t.buf.Write(p)
	for {
		data := t.buf.Bytes()
		idx := bytes.IndexByte(data, '\n')
		if idx < 0 {
			break
		}
		line := string(data[:idx])
		t.buf.Next(idx + 1)
		if strings.TrimSpace(line) != "" && t.onLine != nil {
			t.onLine(line)
		}
	}
	return n, err
}

func (t *logTap) flush() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.buf.Len() == 0 {
		return
	}
	line := strings.TrimRight(t.buf.String(), "\r\n")
	t.buf.Reset()
	if strings.TrimSpace(line) != "" && t.onLine != nil {
		t.onLine(line)
	}
}
