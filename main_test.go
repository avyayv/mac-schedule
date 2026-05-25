package main

import "testing"

func TestBuildScheduleEveryBetween(t *testing.T) {
	s, err := buildSchedule("3h", "09:00-21:00", "")
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

func TestSlug(t *testing.T) {
	if got := slug("Twitter Digest!!"); got != "twitter-digest" {
		t.Fatalf("slug = %q", got)
	}
}
