package middlewares

import (
	"sync"
)

// SSEBroker handles pub/sub for Server-Sent Events
type SSEBroker struct {
	clients map[chan string]bool
	mtx     sync.Mutex
}

var Broker = &SSEBroker{
	clients: make(map[chan string]bool),
}

func (b *SSEBroker) AddClient() chan string {
	b.mtx.Lock()
	defer b.mtx.Unlock()
	ch := make(chan string, 100) // Buffer to prevent blocking
	b.clients[ch] = true
	return ch
}

func (b *SSEBroker) RemoveClient(ch chan string) {
	b.mtx.Lock()
	defer b.mtx.Unlock()
	if _, ok := b.clients[ch]; ok {
		delete(b.clients, ch)
		close(ch)
	}
}

func (b *SSEBroker) Broadcast(msg string) {
	b.mtx.Lock()
	defer b.mtx.Unlock()
	for ch := range b.clients {
		select {
		case ch <- msg:
		default:
			// client is too slow, drop message instead of blocking the whole system
		}
	}
}
