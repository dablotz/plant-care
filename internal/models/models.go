package models

// PlantIdentifyRequest is the incoming request for plant identification.
// Either Name or ImageBase64 must be provided.
type PlantIdentifyRequest struct {
	Name         string `json:"name,omitempty"`
	ImageBase64  string `json:"image_base64,omitempty"`
	ImageMIME    string `json:"image_mime,omitempty"` // e.g. "image/jpeg"
}

// CareScheduleItem represents a single recurring care task.
type CareScheduleItem struct {
	Task            string `json:"task"`             // "Water", "Fertilize", "Repot", etc.
	FrequencyDays   int    `json:"frequency_days"`   // repeat every N days
	Notes           string `json:"notes,omitempty"`  // e.g. "Use distilled water"
	SeasonalNote    string `json:"seasonal_note,omitempty"`
}

// CarePlan is the structured response from Bedrock.
type CarePlan struct {
	PlantName       string             `json:"plant_name"`
	CommonName      string             `json:"common_name,omitempty"`
	ScientificName  string             `json:"scientific_name,omitempty"`
	Summary         string             `json:"summary"`
	Light           string             `json:"light"`
	Humidity        string             `json:"humidity"`
	Temperature     string             `json:"temperature"`
	SoilType        string             `json:"soil_type"`
	Schedule        []CareScheduleItem `json:"schedule"`
	ToxicityNote    string             `json:"toxicity_note,omitempty"`
	ProTips         []string           `json:"pro_tips,omitempty"`
}

// CalendarRequest is sent by the frontend to generate an .ics file.
type CalendarRequest struct {
	CarePlan      CarePlan `json:"care_plan"`
	StartDate     string   `json:"start_date"`     // ISO 8601 date: "2024-06-01"
	// User-selected task overrides: map of task name -> custom frequency in days (0 = omit)
	TaskOverrides map[string]int `json:"task_overrides"`
}
