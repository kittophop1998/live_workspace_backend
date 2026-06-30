package usecase

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"

	"kingdom_manager/backend/internal/domain/entity"
	"kingdom_manager/backend/internal/domain/port"
)

const roomCodeLength = 6

type RoomSession struct {
	Workspace    *entity.Workspace
	Collaborator entity.Collaborator
}

type RoomService struct {
	repo port.WorkspaceRepository
	now  func() time.Time
}

func NewRoomService(repo port.WorkspaceRepository) *RoomService {
	return &RoomService{repo: repo, now: func() time.Time { return time.Now().UTC() }}
}

func (s *RoomService) Create(ctx context.Context, name string) (*RoomSession, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, validation("name is required", nil)
	}
	for range 10 {
		code, err := numericCode(roomCodeLength)
		if err != nil {
			return nil, fmt.Errorf("generate room code: %w", err)
		}
		creator := newCollaborator(name, 0)
		workspace := &entity.Workspace{
			ID:            code,
			Rev:           1,
			Resources:     []entity.Resource{},
			Comments:      []entity.Comment{},
			Collaborators: []entity.Collaborator{creator},
			Activity: []entity.ActivityEvent{{
				ID: "act_" + shortID(), Actor: name, Verb: "created", Target: "room", At: s.now(),
			}},
		}
		if err := s.repo.Create(ctx, workspace); err != nil {
			if errors.Is(err, port.ErrWorkspaceExists) {
				continue
			}
			return nil, fmt.Errorf("create room: %w", err)
		}
		return &RoomSession{Workspace: workspace, Collaborator: creator}, nil
	}
	return nil, fmt.Errorf("generate unique room code: exhausted retries")
}

func (s *RoomService) Join(ctx context.Context, roomCode, name string) (*RoomSession, error) {
	roomCode, name = strings.TrimSpace(roomCode), strings.TrimSpace(name)
	if len(roomCode) != roomCodeLength || !isDigits(roomCode) || name == "" {
		return nil, validation("valid room_code and name are required", nil)
	}
	for range 5 {
		workspace, err := s.repo.Get(ctx, roomCode)
		if err != nil {
			if errors.Is(err, port.ErrWorkspaceNotFound) {
				return nil, notFound("room", roomCode)
			}
			return nil, fmt.Errorf("get room: %w", err)
		}
		for _, collaborator := range workspace.Collaborators {
			if strings.EqualFold(collaborator.Name, name) {
				return &RoomSession{Workspace: workspace, Collaborator: collaborator}, nil
			}
		}
		oldRev := workspace.Rev
		collaborator := newCollaborator(name, len(workspace.Collaborators))
		workspace.Collaborators = append(workspace.Collaborators, collaborator)
		workspace.Rev++
		workspace.Activity = append(workspace.Activity, entity.ActivityEvent{
			ID: "act_" + shortID(), Actor: name, Verb: "joined", Target: "room", At: s.now(),
		})
		if err := s.repo.Save(ctx, workspace, oldRev); err != nil {
			if errors.Is(err, port.ErrRevisionConflict) {
				continue
			}
			return nil, fmt.Errorf("join room: %w", err)
		}
		return &RoomSession{Workspace: workspace, Collaborator: collaborator}, nil
	}
	return nil, &Error{Kind: ErrRevConflict, Message: "room was changed by another client"}
}

func numericCode(length int) (string, error) {
	if length < 1 {
		return "", fmt.Errorf("length must be positive")
	}
	minimum := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(length-1)), nil)
	rangeSize := new(big.Int).Mul(big.NewInt(9), minimum)
	value, err := rand.Int(rand.Reader, rangeSize)
	if err != nil {
		return "", err
	}
	value.Add(value, minimum)
	return value.String(), nil
}

func newCollaborator(name string, index int) entity.Collaborator {
	roles := []entity.CollaboratorRole{entity.RoleBackend, entity.RoleFrontend}
	colors := []string{"#2563EB", "#16A34A", "#9333EA", "#EA580C", "#0891B2", "#DB2777"}
	return entity.Collaborator{
		ID:   "col_" + strings.ReplaceAll(uuid.NewString()[:8], "-", ""),
		Name: name, Role: roles[index%len(roles)], Color: colors[index%len(colors)],
	}
}

func isDigits(value string) bool {
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}
