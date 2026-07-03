package soar

import (
	"encoding/json"
	"testing"
)

func TestAdvancedConfigRoundTrip(t *testing.T) {
	orig := AdvancedConfig{
		TimeZone:     "Asia/Ho_Chi_Minh",
		ScheduleType: ScheduleTypeWeekly,
		WeeklySchedule: &WeeklySchedule{
			Date:     ScheduleDate{Year: 2026, Month: 7, Day: 1},
			Time:     TimeOfDay{Hours: 9, Minutes: 30},
			Days:     []string{"MONDAY", "WEDNESDAY", "FRIDAY"},
			Interval: 1,
		},
	}
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got AdvancedConfig
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.TimeZone != orig.TimeZone {
		t.Errorf("TimeZone = %q, want %q", got.TimeZone, orig.TimeZone)
	}
	if got.ScheduleType != orig.ScheduleType {
		t.Errorf("ScheduleType = %q, want %q", got.ScheduleType, orig.ScheduleType)
	}
	if got.WeeklySchedule == nil {
		t.Fatal("WeeklySchedule is nil after round-trip")
	}
	ws := got.WeeklySchedule
	if ws.Date.Year != 2026 || ws.Date.Month != 7 || ws.Date.Day != 1 {
		t.Errorf("Date = %+v, want 2026-07-01", ws.Date)
	}
	if ws.Time.Hours != 9 || ws.Time.Minutes != 30 {
		t.Errorf("Time = %+v, want 09:30", ws.Time)
	}
	if len(ws.Days) != 3 || ws.Days[0] != "MONDAY" || ws.Days[2] != "FRIDAY" {
		t.Errorf("Days = %v, want [MONDAY WEDNESDAY FRIDAY]", ws.Days)
	}
	if ws.Interval != 1 {
		t.Errorf("Interval = %d, want 1", ws.Interval)
	}
	// Unused schedule types must be nil.
	if got.OneTimeSchedule != nil || got.DailySchedule != nil || got.MonthlySchedule != nil {
		t.Error("unused schedule types should be nil after round-trip")
	}
}

func TestAdvancedConfigRoundTrip_Daily(t *testing.T) {
	orig := AdvancedConfig{
		TimeZone:     "UTC",
		ScheduleType: ScheduleTypeDaily,
		DailySchedule: &DailySchedule{
			Date:     ScheduleDate{Year: 2026, Month: 1, Day: 15},
			Time:     TimeOfDay{Hours: 0, Minutes: 0},
			Interval: 2,
		},
	}
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got AdvancedConfig
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.DailySchedule == nil {
		t.Fatal("DailySchedule is nil after round-trip")
	}
	if got.DailySchedule.Interval != 2 {
		t.Errorf("Interval = %d, want 2", got.DailySchedule.Interval)
	}
}

func TestScheduleTypeConstants(t *testing.T) {
	for _, tc := range []struct {
		got  ScheduleType
		want string
	}{
		{ScheduleTypeUnspecified, "SCHEDULE_TYPE_UNSPECIFIED"},
		{ScheduleTypeOnce, "ONCE"},
		{ScheduleTypeDaily, "DAILY"},
		{ScheduleTypeWeekly, "WEEKLY"},
		{ScheduleTypeMonthly, "MONTHLY"},
	} {
		if string(tc.got) != tc.want {
			t.Errorf("ScheduleType = %q, want %q", tc.got, tc.want)
		}
	}
}
