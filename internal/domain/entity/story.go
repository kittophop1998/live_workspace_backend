package entity

import "time"

type StoryStepType string

const (
	StoryStepEndpoint StoryStepType = "endpoint"
	StoryStepNote     StoryStepType = "note"
	StoryStepSection  StoryStepType = "section"
)

type StoryStep struct {
	ID         string
	Type       StoryStepType
	ResourceID string
	Text       string
}

type Story struct {
	ID          string
	WorkspaceID string
	Name        string
	Steps       []StoryStep
	CreatedAt   time.Time
	CreatedBy   string
	UpdatedAt   time.Time
	UpdatedBy   string
}
