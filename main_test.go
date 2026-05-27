package main

import "testing"

func TestBuildScheduleEveryBetween(t *testing.T) {
	s, err := buildSchedule("3h", "09:00-21:00", "", "")
	if err != nil {
		t.Fatal(err)
	}
	got := describeSchedule(s)
	want := "every 3h between 09:00-21:00 (09:00,12:00,15:00,18:00,21:00)"
	if got != want {
		t.Fatalf("describeSchedule() = %q, want %q", got, want)
	}
}

func TestParseTimesSortsAndDedupes(t *testing.T) {
	times, err := parseTimes("21:00,09:00,12:30,09:00")
	if err != nil {
		t.Fatal(err)
	}
	want := []TimeOfDay{{9, 0}, {12, 30}, {21, 0}}
	if len(times) != len(want) {
		t.Fatalf("len = %d, want %d", len(times), len(want))
	}
	for i := range want {
		if times[i] != want[i] {
			t.Fatalf("times[%d] = %+v, want %+v", i, times[i], want[i])
		}
	}
}

func TestBuildScheduleCron(t *testing.T) {
	s, err := buildSchedule("", "", "", "*/15 9-17 * * MON-FRI")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := describeSchedule(s), "cron */15 9-17 * * MON-FRI"; got != want {
		t.Fatalf("describeSchedule() = %q, want %q", got, want)
	}
	if got, want := len(s.CalendarIntervals), 180; got != want {
		t.Fatalf("len(CalendarIntervals) = %d, want %d", got, want)
	}
	first := s.CalendarIntervals[0]
	if *first.Minute != 0 || *first.Hour != 9 || *first.Weekday != 1 {
		t.Fatalf("first interval = %+v", first)
	}
}

func TestBuildScheduleCronMacro(t *testing.T) {
	s, err := buildSchedule("", "", "", "@daily")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(s.CalendarIntervals), 1; got != want {
		t.Fatalf("len(CalendarIntervals) = %d, want %d", got, want)
	}
	interval := s.CalendarIntervals[0]
	if *interval.Minute != 0 || *interval.Hour != 0 {
		t.Fatalf("interval = %+v", interval)
	}
}

func TestBuildScheduleCronWeekdayRangeThroughSunday(t *testing.T) {
	s, err := buildSchedule("", "", "", "0 9 * * FRI-SUN")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(s.CalendarIntervals), 3; got != want {
		t.Fatalf("len(CalendarIntervals) = %d, want %d", got, want)
	}
}

func TestBuildScheduleCronRejectsDayAndWeekday(t *testing.T) {
	_, err := buildSchedule("", "", "", "0 9 1 * MON")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSlug(t *testing.T) {
	if got := slug("Twitter Digest!!"); got != "twitter-digest" {
		t.Fatalf("slug = %q", got)
	}
}
