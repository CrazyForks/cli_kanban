package model

import "time"

// DailyCompletion represents the number of tasks completed on a given day.
type DailyCompletion struct {
	Date  string
	Count int
}

// TaskStats contains aggregated completion metrics for the current workspace.
type TaskStats struct {
	CompletedToday    int
	CompletedThisWeek int
	CompletedLast7Days []DailyCompletion
	GeneratedAt       time.Time
}

// BuildTaskStats builds completion statistics from a task slice.
func BuildTaskStats(tasks []Task, now time.Time) TaskStats {
	loc := now.Location()
	todayStart := startOfDay(now.In(loc))
	weekStart := startOfWeek(todayStart)

	last7Days := make([]DailyCompletion, 7)
	dayIndex := make(map[string]int, len(last7Days))
	for i := 0; i < len(last7Days); i++ {
		day := todayStart.AddDate(0, 0, -(len(last7Days)-1-i))
		key := day.Format("2006-01-02")
		last7Days[i] = DailyCompletion{
			Date:  key,
			Count: 0,
		}
		dayIndex[key] = i
	}

	stats := TaskStats{
		CompletedLast7Days: last7Days,
		GeneratedAt:        now.In(loc),
	}

	for _, task := range tasks {
		if task.CompletedAt == nil {
			continue
		}

		completedAt := task.CompletedAt.In(loc)
		completedDay := startOfDay(completedAt)

		if completedDay.Equal(todayStart) {
			stats.CompletedToday++
		}
		if !completedDay.Before(weekStart) {
			stats.CompletedThisWeek++
		}

		key := completedDay.Format("2006-01-02")
		if idx, ok := dayIndex[key]; ok {
			stats.CompletedLast7Days[idx].Count++
		}
	}

	return stats
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func startOfWeek(t time.Time) time.Time {
	weekday := int(t.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	return t.AddDate(0, 0, -(weekday - 1))
}
