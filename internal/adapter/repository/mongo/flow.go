package mongo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"kingdom_manager/backend/internal/domain/entity"
	"kingdom_manager/backend/internal/domain/port"
)

// FlowRepository persists E2E workflows + run results in their own collections,
// separate from the rev-guarded workspaces document.
type FlowRepository struct {
	flows *mongo.Collection
	runs  *mongo.Collection
}

func NewFlowRepository(database *mongo.Database) *FlowRepository {
	return &FlowRepository{flows: database.Collection("flows"), runs: database.Collection("flow_runs")}
}

func (r *FlowRepository) EnsureIndexes(ctx context.Context) error {
	if _, err := r.flows.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "workspace_id", Value: 1}, {Key: "created_at", Value: -1}}}); err != nil {
		return err
	}
	if _, err := r.runs.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "flow_id", Value: 1}, {Key: "started_at", Value: -1}}}); err != nil {
		return err
	}
	return nil
}

type flowVariableDoc struct {
	Name  string `bson:"name"`
	In    string `bson:"in"`
	Value any    `bson:"value"`
}
type flowOutputDoc struct {
	Name string `bson:"name"`
	From string `bson:"from"`
}
type stepParameterDoc struct {
	Name  string `bson:"name"`
	In    string `bson:"in"`
	Value any    `bson:"value"`
}
type criterionDoc struct {
	Condition string `bson:"condition"`
	Context   string `bson:"context"`
	Type      string `bson:"type"`
}
type flowStepDoc struct {
	ID              string             `bson:"id"`
	StepID          string             `bson:"step_id"`
	Description     string             `bson:"description"`
	OperationID     string             `bson:"operation_id"`
	Method          string             `bson:"method"`
	Path            string             `bson:"path"`
	Order           int                `bson:"order"`
	DependsOn       []string           `bson:"depends_on"`
	Parameters      []stepParameterDoc `bson:"parameters"`
	RequestBody     any                `bson:"request_body"`
	Outputs         []flowOutputDoc    `bson:"outputs"`
	SuccessCriteria []criterionDoc     `bson:"success_criteria"`
}
type flowDoc struct {
	ID          string            `bson:"_id"`
	WorkspaceID string            `bson:"workspace_id"`
	Name        string            `bson:"name"`
	Description string            `bson:"description"`
	Inputs      []flowVariableDoc `bson:"inputs"`
	Steps       []flowStepDoc     `bson:"steps"`
	CreatedAt   time.Time         `bson:"created_at"`
	CreatedBy   string            `bson:"created_by"`
}

type stepResultDoc struct {
	StepID      string         `bson:"step_id"`
	Method      string         `bson:"method"`
	URL         string         `bson:"url"`
	Status      int            `bson:"status"`
	DurationMs  int64          `bson:"duration_ms"`
	Passed      bool           `bson:"passed"`
	Skipped     bool           `bson:"skipped"`
	Failures    []string       `bson:"failures"`
	Outputs     map[string]any `bson:"outputs"`
	Error          string            `bson:"error"`
	RequestHeaders map[string]string `bson:"request_headers"`
	RequestBody    string            `bson:"request_body"`
	Response       string            `bson:"response"`
}
type flowRunDoc struct {
	ID          string          `bson:"_id"`
	FlowID      string          `bson:"flow_id"`
	WorkspaceID string          `bson:"workspace_id"`
	Status      string          `bson:"status"`
	BaseURL     string          `bson:"base_url"`
	StartedAt   time.Time       `bson:"started_at"`
	FinishedAt  time.Time       `bson:"finished_at"`
	Steps       []stepResultDoc `bson:"steps"`
}

func (r *FlowRepository) CreateFlow(ctx context.Context, flow *entity.FlowDefinition) error {
	if _, err := r.flows.InsertOne(ctx, toFlowDoc(flow)); err != nil {
		return fmt.Errorf("insert flow: %w", err)
	}
	return nil
}

func (r *FlowRepository) ListFlows(ctx context.Context, workspaceID string) ([]entity.FlowDefinition, error) {
	cursor, err := r.flows.Find(ctx, bson.M{"workspace_id": workspaceID}, options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}))
	if err != nil {
		return nil, fmt.Errorf("list flows: %w", err)
	}
	defer cursor.Close(ctx)
	var docs []flowDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("decode flows: %w", err)
	}
	out := make([]entity.FlowDefinition, 0, len(docs))
	for i := range docs {
		out = append(out, *toFlowEntity(&docs[i]))
	}
	return out, nil
}

func (r *FlowRepository) GetFlow(ctx context.Context, id string) (*entity.FlowDefinition, error) {
	var doc flowDoc
	if err := r.flows.FindOne(ctx, bson.M{"_id": id}).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, port.ErrFlowNotFound
		}
		return nil, fmt.Errorf("find flow: %w", err)
	}
	return toFlowEntity(&doc), nil
}

func (r *FlowRepository) DeleteFlow(ctx context.Context, workspaceID, id string) (bool, error) {
	result, err := r.flows.DeleteOne(ctx, bson.M{"_id": id, "workspace_id": workspaceID})
	if err != nil {
		return false, fmt.Errorf("delete flow: %w", err)
	}
	if _, err := r.runs.DeleteMany(ctx, bson.M{"flow_id": id, "workspace_id": workspaceID}); err != nil {
		return result.DeletedCount > 0, fmt.Errorf("delete flow runs: %w", err)
	}
	return result.DeletedCount > 0, nil
}

func (r *FlowRepository) SaveRun(ctx context.Context, run *entity.FlowRun) error {
	if _, err := r.runs.InsertOne(ctx, toRunDoc(run)); err != nil {
		return fmt.Errorf("insert flow run: %w", err)
	}
	return nil
}

func (r *FlowRepository) ListRuns(ctx context.Context, flowID string) ([]entity.FlowRun, error) {
	cursor, err := r.runs.Find(ctx, bson.M{"flow_id": flowID}, options.Find().SetSort(bson.D{{Key: "started_at", Value: -1}}).SetLimit(50))
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer cursor.Close(ctx)
	var docs []flowRunDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("decode runs: %w", err)
	}
	out := make([]entity.FlowRun, 0, len(docs))
	for i := range docs {
		out = append(out, *toRunEntity(&docs[i]))
	}
	return out, nil
}

func (r *FlowRepository) GetRun(ctx context.Context, runID string) (*entity.FlowRun, error) {
	var doc flowRunDoc
	if err := r.runs.FindOne(ctx, bson.M{"_id": runID}).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, port.ErrFlowRunNotFound
		}
		return nil, fmt.Errorf("find flow run: %w", err)
	}
	return toRunEntity(&doc), nil
}

func toFlowDoc(flow *entity.FlowDefinition) flowDoc {
	doc := flowDoc{
		ID: flow.ID, WorkspaceID: flow.WorkspaceID, Name: flow.Name, Description: flow.Description,
		CreatedAt: flow.CreatedAt, CreatedBy: flow.CreatedBy,
		Inputs: make([]flowVariableDoc, 0, len(flow.Inputs)), Steps: make([]flowStepDoc, 0, len(flow.Steps)),
	}
	for _, input := range flow.Inputs {
		doc.Inputs = append(doc.Inputs, flowVariableDoc{Name: input.Name, In: input.In, Value: input.Value})
	}
	for _, step := range flow.Steps {
		item := flowStepDoc{
			ID: step.ID, StepID: step.StepID, Description: step.Description, OperationID: step.OperationID,
			Method: step.Method, Path: step.Path, Order: step.Order, DependsOn: step.DependsOn,
			RequestBody: step.RequestBody,
		}
		for _, param := range step.Parameters {
			item.Parameters = append(item.Parameters, stepParameterDoc{Name: param.Name, In: param.In, Value: param.Value})
		}
		for _, output := range step.Outputs {
			item.Outputs = append(item.Outputs, flowOutputDoc{Name: output.Name, From: output.From})
		}
		for _, criterion := range step.SuccessCriteria {
			item.SuccessCriteria = append(item.SuccessCriteria, criterionDoc{Condition: criterion.Condition, Context: criterion.Context, Type: criterion.Type})
		}
		doc.Steps = append(doc.Steps, item)
	}
	return doc
}

func toFlowEntity(doc *flowDoc) *entity.FlowDefinition {
	flow := &entity.FlowDefinition{
		ID: doc.ID, WorkspaceID: doc.WorkspaceID, Name: doc.Name, Description: doc.Description,
		CreatedAt: doc.CreatedAt, CreatedBy: doc.CreatedBy,
	}
	for _, input := range doc.Inputs {
		flow.Inputs = append(flow.Inputs, entity.FlowVariable{Name: input.Name, In: input.In, Value: normalizeBSONValue(input.Value)})
	}
	for _, step := range doc.Steps {
		item := entity.FlowStep{
			ID: step.ID, StepID: step.StepID, Description: step.Description, OperationID: step.OperationID,
			Method: step.Method, Path: step.Path, Order: step.Order, DependsOn: step.DependsOn,
			RequestBody: normalizeBSONValue(step.RequestBody),
		}
		for _, param := range step.Parameters {
			item.Parameters = append(item.Parameters, entity.StepParameter{Name: param.Name, In: param.In, Value: normalizeBSONValue(param.Value)})
		}
		for _, output := range step.Outputs {
			item.Outputs = append(item.Outputs, entity.FlowOutput{Name: output.Name, From: output.From})
		}
		for _, criterion := range step.SuccessCriteria {
			item.SuccessCriteria = append(item.SuccessCriteria, entity.Criterion{Condition: criterion.Condition, Context: criterion.Context, Type: criterion.Type})
		}
		flow.Steps = append(flow.Steps, item)
	}
	return flow
}

func toRunDoc(run *entity.FlowRun) flowRunDoc {
	doc := flowRunDoc{
		ID: run.ID, FlowID: run.FlowID, WorkspaceID: run.WorkspaceID, Status: string(run.Status),
		BaseURL: run.BaseURL, StartedAt: run.StartedAt, FinishedAt: run.FinishedAt,
		Steps: make([]stepResultDoc, 0, len(run.Steps)),
	}
	for _, step := range run.Steps {
		doc.Steps = append(doc.Steps, stepResultDoc{
			StepID: step.StepID, Method: step.Method, URL: step.URL, Status: step.Status,
			DurationMs: step.DurationMs, Passed: step.Passed, Skipped: step.Skipped, Failures: step.Failures,
			Outputs: step.Outputs, Error: step.Error, RequestHeaders: step.RequestHeaders, RequestBody: step.RequestBody, Response: step.Response,
		})
	}
	return doc
}

func toRunEntity(doc *flowRunDoc) *entity.FlowRun {
	run := &entity.FlowRun{
		ID: doc.ID, FlowID: doc.FlowID, WorkspaceID: doc.WorkspaceID, Status: entity.FlowRunStatus(doc.Status),
		BaseURL: doc.BaseURL, StartedAt: doc.StartedAt, FinishedAt: doc.FinishedAt,
	}
	for _, step := range doc.Steps {
		outputs := map[string]any{}
		for key, value := range step.Outputs {
			outputs[key] = normalizeBSONValue(value)
		}
		run.Steps = append(run.Steps, entity.StepResult{
			StepID: step.StepID, Method: step.Method, URL: step.URL, Status: step.Status,
			DurationMs: step.DurationMs, Passed: step.Passed, Skipped: step.Skipped, Failures: step.Failures,
			Outputs: outputs, Error: step.Error, RequestHeaders: step.RequestHeaders, RequestBody: step.RequestBody, Response: step.Response,
		})
	}
	return run
}
