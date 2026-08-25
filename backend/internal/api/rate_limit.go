package api

import (
	"sync"
	"time"
)

type ExpensiveOperationLimiter interface {
	Allow(userID, operation string, now time.Time) bool
}

type ExpensiveOperationLimits struct {
	MaxPerMinute int
	MaxPerDay    int
}

type InMemoryExpensiveOperationLimiter struct {
	mu     sync.Mutex
	limits ExpensiveOperationLimits
	counts map[string]operationCounters
}

type operationCounters struct {
	minuteWindow time.Time
	minuteCount  int
	dayWindow    time.Time
	dayCount     int
}

func NewInMemoryExpensiveOperationLimiter(limits ExpensiveOperationLimits) *InMemoryExpensiveOperationLimiter {
	if limits.MaxPerMinute <= 0 {
		limits.MaxPerMinute = 12
	}
	if limits.MaxPerDay <= 0 {
		limits.MaxPerDay = 100
	}
	return &InMemoryExpensiveOperationLimiter{limits: limits, counts: make(map[string]operationCounters)}
}

func (l *InMemoryExpensiveOperationLimiter) Allow(userID, operation string, now time.Time) bool {
	if l == nil || userID == "" {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	key := userID + "\x00" + operation
	counter := l.counts[key]
	minuteWindow := now.UTC().Truncate(time.Minute)
	dayWindow := now.UTC().Truncate(24 * time.Hour)
	if !counter.minuteWindow.Equal(minuteWindow) {
		counter.minuteWindow = minuteWindow
		counter.minuteCount = 0
	}
	if !counter.dayWindow.Equal(dayWindow) {
		counter.dayWindow = dayWindow
		counter.dayCount = 0
	}
	if counter.minuteCount >= l.limits.MaxPerMinute || counter.dayCount >= l.limits.MaxPerDay {
		l.counts[key] = counter
		return false
	}
	counter.minuteCount++
	counter.dayCount++
	l.counts[key] = counter
	return true
}
