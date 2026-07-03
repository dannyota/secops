// Schedule types for advanced job instance configuration.

package soar

// ScheduleType enumerates the job scheduling modes.
type ScheduleType string

const (
	ScheduleTypeUnspecified ScheduleType = "SCHEDULE_TYPE_UNSPECIFIED"
	ScheduleTypeOnce        ScheduleType = "ONCE"
	ScheduleTypeDaily       ScheduleType = "DAILY"
	ScheduleTypeWeekly      ScheduleType = "WEEKLY"
	ScheduleTypeMonthly     ScheduleType = "MONTHLY"
)

// AdvancedConfig holds the schedule configuration for a job instance that uses
// advanced (calendar-based) scheduling instead of a simple interval.
type AdvancedConfig struct {
	TimeZone        string           `json:"timeZone,omitempty"`
	ScheduleType    ScheduleType     `json:"scheduleType,omitempty"`
	OneTimeSchedule *OneTimeSchedule `json:"oneTimeSchedule,omitempty"`
	DailySchedule   *DailySchedule   `json:"dailySchedule,omitempty"`
	WeeklySchedule  *WeeklySchedule  `json:"weeklySchedule,omitempty"`
	MonthlySchedule *MonthlySchedule `json:"monthlySchedule,omitempty"`
}

// ScheduleDate is a calendar date (year/month/day).
type ScheduleDate struct {
	Year  int `json:"year"`
	Month int `json:"month"`
	Day   int `json:"day"`
}

// TimeOfDay is a wall-clock time within a day.
type TimeOfDay struct {
	Hours   int `json:"hours"`
	Minutes int `json:"minutes"`
	Seconds int `json:"seconds,omitempty"`
	Nanos   int `json:"nanos,omitempty"`
}

// OneTimeSchedule runs the job once at the specified date and time.
type OneTimeSchedule struct {
	Date ScheduleDate `json:"date"`
	Time TimeOfDay    `json:"time"`
}

// DailySchedule runs the job every N days starting from the given date/time.
type DailySchedule struct {
	Date     ScheduleDate `json:"date"`
	Time     TimeOfDay    `json:"time"`
	Interval int          `json:"interval,omitempty"`
}

// WeeklySchedule runs the job on selected days every N weeks.
type WeeklySchedule struct {
	Date     ScheduleDate `json:"date"`
	Time     TimeOfDay    `json:"time"`
	Days     []string     `json:"days,omitempty"`
	Interval int          `json:"interval,omitempty"`
}

// MonthlySchedule runs the job on a given day every N months.
type MonthlySchedule struct {
	Date     ScheduleDate `json:"date"`
	Time     TimeOfDay    `json:"time"`
	Day      int          `json:"day,omitempty"`
	Interval int          `json:"interval,omitempty"`
}
