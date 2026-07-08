package port

import (
	"context"
	"errors"

	"kingdom_manager/backend/internal/domain/entity"
)

var ErrProposalNotFound = errors.New("proposal not found")

type ProposalRepository interface {
	CreateProposal(context.Context, *entity.Proposal) error
	ListProposals(ctx context.Context, workspaceID string) ([]entity.Proposal, error)
	GetProposal(ctx context.Context, id string) (*entity.Proposal, error)
	UpdateProposal(context.Context, *entity.Proposal) error
	DeleteProposal(ctx context.Context, workspaceID, id string) (bool, error)
}
