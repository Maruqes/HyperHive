package db

import (
	"database/sql"
	"errors"
	"time"
)

// SmartDiskSchedule represents a weekly recurring self-test schedule.
// WeekDay uses Go's time.Weekday (0=Sunday..6=Saturday). Hour is 0-23.
type SmartDiskSchedule struct {
	ID          int64
	WeekDay     time.Weekday
	Hour        int
	TestType    string
	LastRun     sql.NullTime
	Active      bool
	Device      string
	MachineName string
}

// DB is the package-level database connection used by helpers when a
// *sql.DB is not passed explicitly. Set this in your application startup.

// InitSmartDiskDB creates the table used to store recurring smartdisk schedules.
func InitSmartDiskDB() error {
	if DB == nil {
		return errors.New("no database provided")
	}

	stmt := `
	CREATE TABLE IF NOT EXISTS smartdisk_repeat (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		week_day INTEGER NOT NULL,
		hour INTEGER NOT NULL,
		type_of_test TEXT NOT NULL,
		last_run TIMESTAMP,
		active INTEGER NOT NULL DEFAULT 1,
		device TEXT,
		machine_name TEXT
	);`
	_, err := DB.Exec(stmt)
	if err != nil {
		return err
	}

	// If table existed before this change, attempt to add the columns.
	// SQLite's ALTER TABLE ADD COLUMN will return an error if the column exists; ignore those errors.
	_, _ = DB.Exec(`ALTER TABLE smartdisk_repeat ADD COLUMN active INTEGER NOT NULL DEFAULT 1`)
	_, _ = DB.Exec(`ALTER TABLE smartdisk_repeat ADD COLUMN device TEXT`)
	_, _ = DB.Exec(`ALTER TABLE smartdisk_repeat ADD COLUMN machine_name TEXT`)
	return nil
}

// AddSchedule inserts a weekly repeating schedule and returns the new row id.
func AddSchedule(weekDay time.Weekday, hour int, testType, device, machineName string, active bool) (int64, error) {
	if DB == nil {
		return 0, errors.New("no database provided")
	}
	activeInt := 0
	if active {
		activeInt = 1
	}
	res, err := DB.Exec(`INSERT INTO smartdisk_repeat (week_day, hour, type_of_test, active, device, machine_name) VALUES (?, ?, ?, ?, ?, ?)`, int(weekDay), hour, testType, activeInt, device, machineName)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetSchedules returns all saved schedules.
func GetSchedules() ([]SmartDiskSchedule, error) {
	if DB == nil {
		return nil, errors.New("no database provided")
	}
	rows, err := DB.Query(`SELECT id, week_day, hour, type_of_test, last_run, active, device, machine_name FROM smartdisk_repeat`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SmartDiskSchedule
	for rows.Next() {
		var s SmartDiskSchedule
		var weekDayInt int
		var activeInt int
		var device sql.NullString
		var machine sql.NullString
		if err := rows.Scan(&s.ID, &weekDayInt, &s.Hour, &s.TestType, &s.LastRun, &activeInt, &device, &machine); err != nil {
			return nil, err
		}
		s.WeekDay = time.Weekday(weekDayInt)
		s.Active = activeInt != 0
		if device.Valid {
			s.Device = device.String
		}
		if machine.Valid {
			s.MachineName = machine.String
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// UpdateLastRun sets last_run for a schedule.
func UpdateLastRun(id int64, t time.Time) error {
	if DB == nil {
		return errors.New("no database provided")
	}
	_, err := DB.Exec(`UPDATE smartdisk_repeat SET last_run = ? WHERE id = ?`, t, id)
	return err
}

// NextRun returns the next occurrence (after "from") for the schedule.
// The returned time has the same location as "from" and minutes/seconds/nanoseconds zeroed.
func NextRun(s SmartDiskSchedule, from time.Time) time.Time {
	loc := from.Location()
	// build target on the same day then move by delta days
	currentWeekday := int(from.Weekday())
	targetWeekday := int(s.WeekDay)
	days := (targetWeekday - currentWeekday + 7) % 7

	candidate := time.Date(from.Year(), from.Month(), from.Day(), s.Hour, 0, 0, 0, loc).AddDate(0, 0, days)
	if !candidate.After(from) {
		// candidate is not after 'from' -> schedule next week
		candidate = candidate.AddDate(0, 0, 7)
	}
	return candidate
}

// GetDueSchedules returns schedules that should run at the given time `now`.
// It respects the `Active` flag and avoids schedules already run in the same hour
// (based on `LastRun`). This is a read-only helper returning matching rows.
func GetDueSchedules(now time.Time) ([]SmartDiskSchedule, error) {
	if DB == nil {
		return nil, errors.New("no database provided")
	}

	schedules, err := GetSchedules()
	if err != nil {
		return nil, err
	}
	var due []SmartDiskSchedule
	for _, s := range schedules {
		if !s.Active {
			continue
		}
		if now.Weekday() != s.WeekDay || now.Hour() != s.Hour {
			continue
		}
		if s.LastRun.Valid {
			lr := s.LastRun.Time
			if lr.Year() == now.Year() && lr.YearDay() == now.YearDay() && lr.Hour() == now.Hour() {
				// already ran this scheduled hour
				continue
			}
		}
		due = append(due, s)
	}
	return due, nil
}

// GetScheduleByID returns a single schedule by id.
func GetScheduleByID(id int64) (SmartDiskSchedule, error) {
	if DB == nil {
		return SmartDiskSchedule{}, errors.New("no database provided")
	}
	row := DB.QueryRow(`SELECT id, week_day, hour, type_of_test, last_run, active, device, machine_name FROM smartdisk_repeat WHERE id = ?`, id)
	var s SmartDiskSchedule
	var weekDayInt int
	var activeInt int
	var device sql.NullString
	var machine sql.NullString
	if err := row.Scan(&s.ID, &weekDayInt, &s.Hour, &s.TestType, &s.LastRun, &activeInt, &device, &machine); err != nil {
		return SmartDiskSchedule{}, err
	}
	s.WeekDay = time.Weekday(weekDayInt)
	s.Active = activeInt != 0
	if device.Valid {
		s.Device = device.String
	}
	if machine.Valid {
		s.MachineName = machine.String
	}
	return s, nil
}

// UpdateSchedule updates all editable fields of a schedule.
func UpdateSchedule(s SmartDiskSchedule) error {
	if DB == nil {
		return errors.New("no database provided")
	}
	activeInt := 0
	if s.Active {
		activeInt = 1
	}
	_, err := DB.Exec(`UPDATE smartdisk_repeat SET week_day = ?, hour = ?, type_of_test = ?, active = ?, device = ?, machine_name = ? WHERE id = ?`, int(s.WeekDay), s.Hour, s.TestType, activeInt, s.Device, s.MachineName, s.ID)
	return err
}

// DeleteSchedule removes a schedule by id.
func DeleteSchedule(id int64) error {
	if DB == nil {
		return errors.New("no database provided")
	}
	_, err := DB.Exec(`DELETE FROM smartdisk_repeat WHERE id = ?`, id)
	return err
}

// SetActive sets the active flag for a schedule.
func SetActive(id int64, active bool) error {
	if DB == nil {
		return errors.New("no database provided")
	}
	activeInt := 0
	if active {
		activeInt = 1
	}
	_, err := DB.Exec(`UPDATE smartdisk_repeat SET active = ? WHERE id = ?`, activeInt, id)
	return err
}
