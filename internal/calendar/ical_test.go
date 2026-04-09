package calendar

import (
	"strings"
	"testing"
	"time"

	"github.com/dablotz/plantcare/internal/models"
)

func TestEscapeICS(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "hello"},
		{"semi;colon", `semi\;colon`},
		{"com,ma", `com\,ma`},
		{"new\nline", `new\nline`},
		{`back\slash`, `back\\slash`},
		{`mix;,` + "\n" + `\`, `mix\;\,\n\\`},
	}
	for _, tt := range tests {
		got := escapeICS(tt.input)
		if got != tt.want {
			t.Errorf("escapeICS(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestGenerateICS_Basic(t *testing.T) {
	plan := models.CarePlan{
		PlantName: "Monstera",
		Schedule: []models.CareScheduleItem{
			{Task: "Watering", FrequencyDays: 7, Notes: "Use filtered water"},
			{Task: "Fertilizing", FrequencyDays: 30},
		},
	}
	start := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	ics, err := GenerateICS(plan, start, nil)
	if err != nil {
		t.Fatalf("GenerateICS returned error: %v", err)
	}

	for _, want := range []string{
		"BEGIN:VCALENDAR",
		"END:VCALENDAR",
		"BEGIN:VEVENT",
		"END:VEVENT",
		"RRULE:FREQ=DAILY;INTERVAL=7",
		"RRULE:FREQ=DAILY;INTERVAL=30",
		"DTSTART;VALUE=DATE:20240601",
		"Watering",
		"Fertilizing",
		"Monstera",
	} {
		if !strings.Contains(ics, want) {
			t.Errorf("expected ICS to contain %q", want)
		}
	}
}

func TestGenerateICS_SkipTask(t *testing.T) {
	plan := models.CarePlan{
		PlantName: "Pothos",
		Schedule: []models.CareScheduleItem{
			{Task: "Watering", FrequencyDays: 7},
			{Task: "Fertilizing", FrequencyDays: 30},
		},
	}
	start := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	overrides := map[string]int{"Fertilizing": 0}

	ics, err := GenerateICS(plan, start, overrides)
	if err != nil {
		t.Fatalf("GenerateICS returned error: %v", err)
	}

	if strings.Contains(ics, "Fertilizing") {
		t.Error("expected Fertilizing to be skipped but found it in ICS")
	}
	if !strings.Contains(ics, "Watering") {
		t.Error("expected Watering to be present in ICS")
	}
}

func TestGenerateICS_Override(t *testing.T) {
	plan := models.CarePlan{
		PlantName: "Fern",
		Schedule: []models.CareScheduleItem{
			{Task: "Watering", FrequencyDays: 7},
		},
	}
	start := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	overrides := map[string]int{"Watering": 14}

	ics, err := GenerateICS(plan, start, overrides)
	if err != nil {
		t.Fatalf("GenerateICS returned error: %v", err)
	}

	if !strings.Contains(ics, "RRULE:FREQ=DAILY;INTERVAL=14") {
		t.Error("expected overridden frequency of 14 days")
	}
	if strings.Contains(ics, "RRULE:FREQ=DAILY;INTERVAL=7") {
		t.Error("expected default frequency to be overridden")
	}
}

func TestGenerateICS_EmptySchedule(t *testing.T) {
	plan := models.CarePlan{
		PlantName: "Empty",
		Schedule:  []models.CareScheduleItem{},
	}
	start := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	ics, err := GenerateICS(plan, start, nil)
	if err != nil {
		t.Fatalf("GenerateICS returned error: %v", err)
	}

	if strings.Contains(ics, "BEGIN:VEVENT") {
		t.Error("expected no events for empty schedule")
	}
	if !strings.Contains(ics, "BEGIN:VCALENDAR") {
		t.Error("expected calendar wrapper to be present")
	}
}

func TestGenerateICS_SeasonalNote(t *testing.T) {
	plan := models.CarePlan{
		PlantName: "Orchid",
		Schedule: []models.CareScheduleItem{
			{Task: "Watering", FrequencyDays: 7, SeasonalNote: "Less water in winter"},
		},
	}
	start := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	ics, err := GenerateICS(plan, start, nil)
	if err != nil {
		t.Fatalf("GenerateICS returned error: %v", err)
	}

	if !strings.Contains(ics, "Less water in winter") {
		t.Error("expected seasonal note in ICS description")
	}
}

func TestGoogleCalendarLinks_Basic(t *testing.T) {
	plan := models.CarePlan{
		PlantName: "Snake Plant",
		Schedule: []models.CareScheduleItem{
			{Task: "Watering", FrequencyDays: 14},
			{Task: "Fertilizing", FrequencyDays: 60},
		},
	}
	start := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	links := GoogleCalendarLinks(plan, start, nil)

	if len(links) != 2 {
		t.Fatalf("expected 2 links, got %d", len(links))
	}
	for task, link := range links {
		if !strings.HasPrefix(link, "https://calendar.google.com/calendar/render?") {
			t.Errorf("link for %q does not start with expected base URL: %q", task, link)
		}
	}
}

func TestGoogleCalendarLinks_SkipTask(t *testing.T) {
	plan := models.CarePlan{
		PlantName: "Cactus",
		Schedule: []models.CareScheduleItem{
			{Task: "Watering", FrequencyDays: 30},
			{Task: "Fertilizing", FrequencyDays: 90},
		},
	}
	start := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	overrides := map[string]int{"Fertilizing": 0}

	links := GoogleCalendarLinks(plan, start, overrides)

	if _, ok := links["Fertilizing"]; ok {
		t.Error("expected Fertilizing to be skipped")
	}
	if _, ok := links["Watering"]; !ok {
		t.Error("expected Watering link to be present")
	}
}

func TestGoogleCalendarLinks_ContainsRRULE(t *testing.T) {
	plan := models.CarePlan{
		PlantName: "Pothos",
		Schedule: []models.CareScheduleItem{
			{Task: "Watering", FrequencyDays: 7},
		},
	}
	start := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	links := GoogleCalendarLinks(plan, start, nil)

	link := links["Watering"]
	if !strings.Contains(link, "FREQ%3DDAILY") && !strings.Contains(link, "FREQ=DAILY") {
		t.Errorf("expected RRULE with FREQ=DAILY in link: %q", link)
	}
}
