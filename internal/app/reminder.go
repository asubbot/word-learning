package app

import (
	"context"
	"time"
)

func (s *Service) ReminderEligible(ctx context.Context, telegramUserID int64, now time.Time, minOverdue int, minHoursSinceReview float64) (eligible bool, overdueCount int, err error) {
	overdueCount, lastReviewedAt, err := s.store.UserOverdueAndLastReview(ctx, telegramUserID, now)
	if err != nil {
		return false, 0, err
	}
	if overdueCount < minOverdue {
		return false, overdueCount, nil
	}
	if lastReviewedAt == nil {
		return true, overdueCount, nil
	}
	elapsed := now.Sub(*lastReviewedAt)
	if elapsed < time.Duration(minHoursSinceReview*float64(time.Hour)) {
		return false, overdueCount, nil
	}
	return true, overdueCount, nil
}
