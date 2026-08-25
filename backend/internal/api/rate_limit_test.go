package api

import (
	"testing"
	"time"
)

func TestInMemoryExpensiveOperationLimiterEnforcesPerUserLimits(t *testing.T) {
	t.Parallel()
	limiter := NewInMemoryExpensiveOperationLimiter(ExpensiveOperationLimits{MaxPerMinute: 2, MaxPerDay: 3})
	now := time.Date(2026, time.August, 26, 10, 0, 0, 0, time.UTC)
	if !limiter.Allow("user-1", "create_story", now) || !limiter.Allow("user-1", "create_story", now) {
		t.Fatal("first two operations should be allowed")
	}
	if limiter.Allow("user-1", "create_story", now) {
		t.Fatal("third operation in the same minute should be rejected")
	}
	if !limiter.Allow("user-1", "create_story", now.Add(time.Minute)) {
		t.Fatal("new minute should allow until daily quota")
	}
	if limiter.Allow("user-1", "create_story", now.Add(2*time.Minute)) {
		t.Fatal("daily quota should be enforced")
	}
	if !limiter.Allow("user-2", "create_story", now) {
		t.Fatal("limits should be scoped per user")
	}
}
