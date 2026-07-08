package entity

import "time"

type ProposalStatus string

const (
	ProposalStatusDraft     ProposalStatus = "draft"
	ProposalStatusReviewing ProposalStatus = "reviewing"
	ProposalStatusApproved  ProposalStatus = "approved"
	ProposalStatusRejected  ProposalStatus = "rejected"
	ProposalStatusMerged    ProposalStatus = "merged"
)

type ProposalComment struct {
	ID       string
	FieldKey string
	Author   string
	Role     CollaboratorRole
	Body     string
	Resolved bool
	At       time.Time
}

type ProposalTimelineEntry struct {
	ID     string
	Kind   string
	Actor  string
	Detail string
	At     time.Time
}

// Proposal is a Pull-Request-like draft of a resource's request-body schema:
// an independent copy of Fields that a team reviews via Comments/Timeline
// before merging the diff back into the real resource.
type Proposal struct {
	ID          string
	WorkspaceID string
	ResourceID  string
	Title       string
	Description string
	Author      string
	AuthorRole  CollaboratorRole
	Status      ProposalStatus
	Fields      []SchemaField
	Comments    []ProposalComment
	Timeline    []ProposalTimelineEntry
	CreatedAt   time.Time
	UpdatedAt   time.Time
	UpdatedBy   string
}
