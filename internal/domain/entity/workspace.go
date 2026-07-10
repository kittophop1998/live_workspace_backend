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

type EndpointStatus string

const (
	EndpointStatusDraft      EndpointStatus = "draft"
	EndpointStatusInProgress EndpointStatus = "inprogress"
	EndpointStatusTesting    EndpointStatus = "testing"
	EndpointStatusDone       EndpointStatus = "done"
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

type FieldValidation struct {
	MinLength *int
	MaxLength *int
	Minimum   *float64
	Maximum   *float64
	Pattern   *string
	Format    *string
}

// SchemaField is a request/response body field. It is recursive: `Children`
// holds nested properties for an "object" field, `Items` holds the element
// schema for an "array" field. Leaf (scalar) fields leave both nil/empty, so
// documents written before nesting was added decode unchanged.
type SchemaField struct {
	ID          string
	Key         string
	Type        string
	Required    bool
	Nullable    bool
	State       FieldState
	Change      FieldChange
	Description *string
	Value       any
	Example     any
	Default     any
	EnumValues  []string
	Validation  *FieldValidation
	Children    []SchemaField
	Items       *SchemaField
}

type ResponseSchema struct {
	Status      int
	Description *string
	Fields      []SchemaField
}

type Resource struct {
	ID        string
	Name      string
	Kind      ResourceKind
	Method    *string
	Path      *string
	State     FieldState
	Status    *EndpointStatus
	Fields    []SchemaField
	Responses []ResponseSchema
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

// ChatMessage is a project-wide team chat message. Chat lives outside the
// rev'd workspace aggregate: it is append-only and never conflicts, so sending
// a message must not bump Rev or copy the workspace children.
type ChatMessage struct {
	ID       string
	AuthorID string
	Author   string
	Role     CollaboratorRole
	Body     string
	At       time.Time
}

// TaskLogKind categorizes a backend work-update entry so the frontend can
// badge/filter it (what the backend added, changed, fixed, removed, or a plain note).
type TaskLogKind string

const (
	TaskLogAdded   TaskLogKind = "added"
	TaskLogChanged TaskLogKind = "changed"
	TaskLogFixed   TaskLogKind = "fixed"
	TaskLogRemoved TaskLogKind = "removed"
	TaskLogNote    TaskLogKind = "note"
)

// TaskLog is a manually-authored backend work-update entry ("I added X / changed
// Y"), broadcast to the room so the frontend knows what the backend has shipped.
// Like ChatMessage it is append-only and lives outside the rev'd workspace
// aggregate, so posting one never bumps Rev or hits a revision conflict. It may
// optionally reference a Resource via ResourceID (empty = workspace-wide note).
type TaskLog struct {
	ID         string
	AuthorID   string
	Author     string
	Role       CollaboratorRole
	Kind       TaskLogKind
	Body       string
	ResourceID string
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
