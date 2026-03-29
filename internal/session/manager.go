package session

import (
	"fmt"
	"sync"
	"time"
)

// Session представляет активную VPN сессию
type Session struct {
	ID           string
	ClientAddr   string
	CreatedAt    time.Time
	LastActivity time.Time

	// Буферы для упорядоченной сборки фрагментированных пакетов
	UpstreamChunks   map[int][]byte // sequence -> data
	DownstreamQueue  [][]byte       // очередь пакетов для отправки клиенту
	DownstreamIndex  int            // текущий индекс для polling

	mu sync.RWMutex
}

// Manager управляет активными сессиями
type Manager struct {
	sessions      map[string]*Session
	mu            sync.RWMutex
	cleanupTicker *time.Ticker
	stopChan      chan struct{}
}

// NewManager создаёт менеджер сессий
func NewManager(cleanupInterval time.Duration) *Manager {
	m := &Manager{
		sessions: make(map[string]*Session),
		stopChan: make(chan struct{}),
	}

	// Запускаем фоновую очистку старых сессий
	if cleanupInterval > 0 {
		m.cleanupTicker = time.NewTicker(cleanupInterval)
		go m.cleanupLoop()
	}

	return m
}

// GetOrCreate получает существующую сессию или создаёт новую
func (m *Manager) GetOrCreate(sessionID, clientAddr string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	if session, exists := m.sessions[sessionID]; exists {
		session.mu.Lock()
		session.LastActivity = time.Now()
		session.mu.Unlock()
		return session
	}

	// Создаём новую сессию
	session := &Session{
		ID:              sessionID,
		ClientAddr:      clientAddr,
		CreatedAt:       time.Now(),
		LastActivity:    time.Now(),
		UpstreamChunks:  make(map[int][]byte),
		DownstreamQueue: make([][]byte, 0),
		DownstreamIndex: 0,
	}

	m.sessions[sessionID] = session
	return session
}

// Get возвращает сессию по ID
func (m *Manager) Get(sessionID string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session, exists := m.sessions[sessionID]
	if exists {
		session.mu.Lock()
		session.LastActivity = time.Now()
		session.mu.Unlock()
	}

	return session, exists
}

// Delete удаляет сессию
func (m *Manager) Delete(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sessionID)
}

// AddUpstreamChunk добавляет chunk upstream данных
func (s *Session) AddUpstreamChunk(sequence int, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.UpstreamChunks[sequence] = data
	s.LastActivity = time.Now()
}

// TryAssembleUpstream пытается собрать полный пакет из chunks
// Возвращает пакет если все chunks получены по порядку
func (s *Session) TryAssembleUpstream(expectedChunks int) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Проверяем что все chunks от 0 до expectedChunks-1 получены
	if len(s.UpstreamChunks) < expectedChunks {
		return nil, false
	}

	for i := 0; i < expectedChunks; i++ {
		if _, exists := s.UpstreamChunks[i]; !exists {
			return nil, false
		}
	}

	// Собираем пакет
	var fullPacket []byte
	for i := 0; i < expectedChunks; i++ {
		fullPacket = append(fullPacket, s.UpstreamChunks[i]...)
	}

	// Очищаем chunks после сборки
	s.UpstreamChunks = make(map[int][]byte)

	return fullPacket, true
}

// EnqueueDownstream добавляет пакет в очередь для отправки клиенту
func (s *Session) EnqueueDownstream(packet []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.DownstreamQueue = append(s.DownstreamQueue, packet)
	s.LastActivity = time.Now()
}

// GetDownstreamPacket возвращает следующий пакет из очереди
func (s *Session) GetDownstreamPacket() ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.DownstreamIndex >= len(s.DownstreamQueue) {
		return nil, false
	}

	packet := s.DownstreamQueue[s.DownstreamIndex]
	s.DownstreamIndex++
	s.LastActivity = time.Now()

	// Очищаем старые пакеты если накопилось много
	if s.DownstreamIndex > 100 {
		s.DownstreamQueue = s.DownstreamQueue[s.DownstreamIndex:]
		s.DownstreamIndex = 0
	}

	return packet, true
}

// GetStats возвращает статистику сессии
func (s *Session) GetStats() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	age := time.Since(s.CreatedAt)
	idle := time.Since(s.LastActivity)

	return fmt.Sprintf("Session %s: age=%v, idle=%v, upstream_chunks=%d, downstream_queue=%d",
		s.ID, age.Round(time.Second), idle.Round(time.Second),
		len(s.UpstreamChunks), len(s.DownstreamQueue))
}

// cleanupLoop периодически удаляет неактивные сессии
func (m *Manager) cleanupLoop() {
	for {
		select {
		case <-m.cleanupTicker.C:
			m.cleanupStale(5 * time.Minute) // удаляем сессии старше 5 минут
		case <-m.stopChan:
			return
		}
	}
}

// cleanupStale удаляет сессии неактивные дольше указанного времени
func (m *Manager) cleanupStale(maxIdle time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for id, session := range m.sessions {
		session.mu.RLock()
		idle := now.Sub(session.LastActivity)
		session.mu.RUnlock()

		if idle > maxIdle {
			delete(m.sessions, id)
		}
	}
}

// Stop останавливает менеджер
func (m *Manager) Stop() {
	if m.cleanupTicker != nil {
		m.cleanupTicker.Stop()
	}
	close(m.stopChan)
}

// Count возвращает количество активных сессий
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// GetAllSessions возвращает все активные сессии
func (m *Manager) GetAllSessions() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sessions := make([]*Session, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	return sessions
}
