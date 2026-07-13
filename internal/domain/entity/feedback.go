package entity

import "time"

type FeedbackStatus string

const (
	FeedbackStatusOpen       FeedbackStatus = "open"
	FeedbackStatusInProgress FeedbackStatus = "in_progress"
	FeedbackStatusResolved   FeedbackStatus = "resolved"
	FeedbackStatusDismissed  FeedbackStatus = "dismissed"
)

type FeedbackCategory string

const (
	FeedbackCategoryComplaint   FeedbackCategory = "complaint"
	FeedbackCategoryImprovement FeedbackCategory = "improvement"
	FeedbackCategoryBug         FeedbackCategory = "bug"
	FeedbackCategoryOther       FeedbackCategory = "other"
)

// Feedback is a workspace-scoped usage report — a complaint, an improvement
// request, or a bug — submitted by any collaborator and tracked through a
// simple status lifecycle (open → in_progress → resolved/dismissed).
type Feedback struct {
	ID          string
	WorkspaceID string
	Category    FeedbackCategory
	Body        string
	Author      string
	AuthorRole  CollaboratorRole
	Status      FeedbackStatus
	CreatedAt   time.Time
	UpdatedAt   time.Time
	UpdatedBy   string
}
