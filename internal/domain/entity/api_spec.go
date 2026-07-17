package entity

import "time"

// APISpecRevision is immutable exact OpenAPI content stored by project.
type APISpecRevision struct {
	ID, ProjectID, PreviousRevisionID, Status, SourceFilename, Format, Content, ContentHash, Message, GitBranch, GitCommitSHA, CreatedByTokenID string
	Number                                                                                                                                      int64
	CreatedAt                                                                                                                                   time.Time
}
