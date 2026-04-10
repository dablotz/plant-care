package calendar

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/dablotz/plantcare/internal/models"
	"github.com/google/uuid"
)

const icsTimeFormat = "20060102T150405Z"
const icsDateFormat = "20060102"

// GenerateICS produces a VCALENDAR .ics file for the given care plan and config.
// startDate is the first occurrence date for all tasks.
// taskOverrides is a map of task name -> frequency in days; 0 means skip that task.
// If taskOverrides is nil/empty, the plan's default frequencies are used.
func GenerateICS(plan models.CarePlan, startDate time.Time, taskOverrides map[string]int) (string, error) {
	var sb strings.Builder

	sb.WriteString("BEGIN:VCALENDAR\r\n")
	sb.WriteString("VERSION:2.0\r\n")
	sb.WriteString("PRODID:-//PlantCare//PlantCare//EN\r\n")
	sb.WriteString("CALSCALE:GREGORIAN\r\n")
	sb.WriteString("METHOD:PUBLISH\r\n")
	sb.WriteString("X-WR-CALNAME:Plant Care - " + escapeICS(plan.PlantName) + "\r\n")
	sb.WriteString("X-WR-TIMEZONE:UTC\r\n")

	now := time.Now().UTC()
	dtstamp := now.Format(icsTimeFormat)

	for _, item := range plan.Schedule {
		freq := item.FrequencyDays

		// Apply user override if present
		if taskOverrides != nil {
			if override, ok := taskOverrides[item.Task]; ok {
				if override == 0 {
					continue // user chose to skip this task
				}
				freq = override
			}
		}

		if freq <= 0 {
			continue
		}

		uid := uuid.New().String() + "@plantcare"
		dtstart := startDate.UTC().Format(icsDateFormat)

		// Build description
		desc := fmt.Sprintf("%s - %s", plan.PlantName, item.Task)
		if item.Notes != "" {
			desc += "\\n" + item.Notes
		}
		if item.SeasonalNote != "" {
			desc += "\\n⚠ Seasonal note: " + item.SeasonalNote
		}

		// RRULE: repeat every N days, indefinitely
		rrule := fmt.Sprintf("RRULE:FREQ=DAILY;INTERVAL=%d", freq)

		sb.WriteString("BEGIN:VEVENT\r\n")
		sb.WriteString("UID:" + uid + "\r\n")
		sb.WriteString("DTSTAMP:" + dtstamp + "\r\n")
		sb.WriteString("DTSTART;VALUE=DATE:" + dtstart + "\r\n")
		sb.WriteString(rrule + "\r\n")
		sb.WriteString("SUMMARY:" + escapeICS(item.Task+" – "+plan.PlantName) + "\r\n")
		sb.WriteString("DESCRIPTION:" + escapeICS(desc) + "\r\n")
		sb.WriteString("CATEGORIES:Plant Care\r\n")
		// Reminder alarm: 9am on the day
		sb.WriteString("BEGIN:VALARM\r\n")
		sb.WriteString("TRIGGER;RELATED=START:PT9H\r\n")
		sb.WriteString("ACTION:DISPLAY\r\n")
		sb.WriteString("DESCRIPTION:Time to " + escapeICS(item.Task) + " your " + escapeICS(plan.PlantName) + "\r\n")
		sb.WriteString("END:VALARM\r\n")
		sb.WriteString("END:VEVENT\r\n")
	}

	sb.WriteString("END:VCALENDAR\r\n")
	return sb.String(), nil
}

// GoogleCalendarLinks returns a map of task name -> Google Calendar "add event" URL
// for one-click adding of the first occurrence of each task.
func GoogleCalendarLinks(plan models.CarePlan, startDate time.Time, taskOverrides map[string]int) map[string]string {
	links := make(map[string]string)

	for _, item := range plan.Schedule {
		freq := item.FrequencyDays
		if taskOverrides != nil {
			if override, ok := taskOverrides[item.Task]; ok {
				if override == 0 {
					continue
				}
				freq = override
			}
		}
		if freq <= 0 {
			continue
		}

		title := fmt.Sprintf("%s – %s", item.Task, plan.PlantName)
		details := item.Notes
		if item.SeasonalNote != "" {
			details += "\nSeasonal note: " + item.SeasonalNote
		}

		// Google Calendar uses YYYYMMDD for all-day events
		dateStr := startDate.UTC().Format("20060102")

		params := url.Values{}
		params.Set("action", "TEMPLATE")
		params.Set("text", title)
		params.Set("dates", dateStr+"/"+dateStr)
		params.Set("details", details)
		params.Set("recur", fmt.Sprintf("RRULE:FREQ=DAILY;INTERVAL=%d", freq))

		links[item.Task] = "https://calendar.google.com/calendar/render?" + params.Encode()
	}

	return links
}

// escapeICS escapes special characters for iCalendar text values per RFC 5545.
func escapeICS(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, ";", `\;`)
	s = strings.ReplaceAll(s, ",", `\,`)
	s = strings.ReplaceAll(s, "\r\n", `\n`) // CRLF pair must come before lone \r and \n
	s = strings.ReplaceAll(s, "\r", `\n`)   // lone carriage return
	s = strings.ReplaceAll(s, "\n", `\n`)   // lone newline
	return s
}
