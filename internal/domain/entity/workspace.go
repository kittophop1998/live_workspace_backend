package entity

import "time"

type CollaboratorRole string

const (
	RoleBackend  CollaboratorRole = "backend"
	RoleFrontend CollaboratorRole = "frontend"
)

type ResourceKind string

const (
	KindEndpoint ResourceKind = "endpoint"
	KindDatabase ResourceKind = "database"
	KindModel    ResourceKind = "model"
)

type FieldState string

const (
	StateDraft    FieldState = "draft"
	StateReady    FieldState = "ready"
	StateBreaking FieldState = "breaking"
)

type FieldChange string

const (
	ChangeStable   FieldChange = "stable"
	ChangeAdded    FieldChange = "added"
	ChangeModified FieldChange = "modified"
	ChangeRemoved  FieldChange = "removed"
)

type Collaborator struct {
	ID    string
	Name  string
	Role  CollaboratorRole
	Color string
}

type SchemaField struct {
	ID          string
	Key         string
	Type        string
	Required    bool
	State       FieldState
	Change      FieldChange
	Description *string
	Value       any
}

type Resource struct {
	ID        string
	Name      string
	Kind      ResourceKind
	Method    *string
	Path      *string
	State     FieldState
	Fields    []SchemaField
	UpdatedAt time.Time
	UpdatedBy string
}

type Comment struct {
	ID         string
	ResourceID string
	FieldID    *string
	AuthorID   string
	Author     string
	Role       CollaboratorRole
	Body       string
	At         time.Time
}

type ActivityEvent struct {
	ID         string
	Actor      string
	Verb       string
	Target     string
	ResourceID string
	At         time.Time
}

type Workspace struct {
	ID            string
	Rev           int64
	Resources     []Resource
	Comments      []Comment
	Activity      []ActivityEvent
	Collaborators []Collaborator
}

func (r *Resource) RollupState() {
	state := StateReady
	for _, field := range r.Fields {
		if field.Change == ChangeRemoved {
			continue
		}
		if field.State == StateBreaking {
			r.State = StateBreaking
			return
		}
		if field.State == StateDraft {
			state = StateDraft
		}
	}
	r.State = state
}
