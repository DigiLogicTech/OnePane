package artifact

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/id"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type eventAppender interface {
	Append(context.Context, storage.Tx, event.Event) error
}

type Service struct {
	tx     storage.Transactor
	repo   repository
	store  BlobStore
	events eventAppender
	ids    id.Generator
	clock  clock.Clock
}

func NewService(db *sql.DB, tx storage.Transactor, store BlobStore, clk clock.Clock) *Service {
	return &Service{tx: tx, repo: newSQLRepository(db), store: store, events: event.Store{}, ids: id.Generator{}, clock: clk}
}

func (s *Service) Get(ctx context.Context, artifactID string) (Artifact, error) {
	if strings.TrimSpace(artifactID) == "" {
		return Artifact{}, fmt.Errorf("%w: artifact id is required", ErrInvalidCommand)
	}
	return s.repo.Get(ctx, artifactID)
}

func (s *Service) Create(ctx context.Context, cmd CreateCommand, content io.Reader) (Artifact, error) {
	if s == nil || s.store == nil || content == nil {
		return Artifact{}, fmt.Errorf("%w: artifact store and content are required", ErrInvalidCommand)
	}
	if strings.TrimSpace(cmd.WorkspaceID) == "" || strings.TrimSpace(cmd.MediaType) == "" {
		return Artifact{}, fmt.Errorf("%w: workspace and media type are required", ErrInvalidCommand)
	}
	if cmd.Label.WorkspaceID != cmd.WorkspaceID {
		return Artifact{}, fmt.Errorf("%w: data label workspace mismatch", ErrInvalidCommand)
	}
	if err := policy.ValidateDataLabel(cmd.Label); err != nil {
		return Artifact{}, fmt.Errorf("%w: %v", ErrInvalidCommand, err)
	}
	if cmd.Status == "" {
		cmd.Status = StatusActive
	}
	if !ValidStatus(cmd.Status) {
		return Artifact{}, fmt.Errorf("%w: invalid status %q", ErrInvalidCommand, cmd.Status)
	}
	if len(cmd.Metadata) == 0 {
		cmd.Metadata = json.RawMessage(`{}`)
	}
	if !json.Valid(cmd.Metadata) {
		return Artifact{}, fmt.Errorf("%w: metadata must be valid JSON", ErrInvalidCommand)
	}

	blob, err := s.store.Put(ctx, content)
	if err != nil {
		return Artifact{}, err
	}
	artifactID, err := s.ids.New("artifact")
	if err != nil {
		return Artifact{}, err
	}
	eventID, err := s.ids.New("evt")
	if err != nil {
		return Artifact{}, err
	}
	now := s.clock.UnixMilli()
	a := Artifact{
		ID: artifactID, WorkspaceID: cmd.WorkspaceID, ProjectID: cmd.ProjectID,
		ContentHash: blob.ContentHash, MediaType: strings.TrimSpace(cmd.MediaType), SizeBytes: blob.SizeBytes,
		StorageRef: blob.StorageRef, Label: cmd.Label, Status: cmd.Status,
		Metadata: append(json.RawMessage(nil), cmd.Metadata...), CreatedBy: cmd.CreatedBy, CreatedAt: now,
	}

	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		status, err := s.repo.WorkspaceStatus(ctx, tx, a.WorkspaceID)
		if err != nil {
			return fmt.Errorf("resolve artifact workspace: %w", err)
		}
		if status != "active" {
			return ErrWorkspaceInactive
		}
		if a.ProjectID != nil {
			if strings.TrimSpace(*a.ProjectID) == "" {
				return fmt.Errorf("%w: project id cannot be blank", ErrInvalidCommand)
			}
			workspaceID, err := s.repo.ProjectWorkspace(ctx, tx, *a.ProjectID)
			if err != nil {
				return fmt.Errorf("resolve artifact project: %w", err)
			}
			if workspaceID != a.WorkspaceID {
				return ErrProjectWorkspace
			}
		}
		if a.CreatedBy != nil {
			if strings.TrimSpace(*a.CreatedBy) == "" {
				return fmt.Errorf("%w: created_by cannot be blank", ErrInvalidCommand)
			}
			eligible, err := s.repo.PrincipalEligible(ctx, tx, a.WorkspaceID, *a.CreatedBy)
			if err != nil {
				return fmt.Errorf("resolve artifact creator: %w", err)
			}
			if !eligible {
				return ErrCreatorIneligible
			}
		}
		if err := s.repo.Insert(ctx, tx, a); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{
			"artifact_id": a.ID, "content_hash": a.ContentHash, "size_bytes": a.SizeBytes,
			"media_type": a.MediaType, "storage_ref": a.StorageRef, "status": a.Status,
		})
		actor := cmd.ActorPrincipalID
		if actor == nil {
			actor = cmd.CreatedBy
		}
		return s.events.Append(ctx, tx, event.Event{
			ID: eventID, WorkspaceID: &a.WorkspaceID, Type: "artifact.created",
			AggregateType: "artifact", AggregateID: a.ID, ActorPrincipalID: actor,
			RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now,
		})
	})
	if err != nil {
		// The blob is immutable and content-addressed. A metadata transaction
		// failure may leave an unreferenced blob; future GC can safely collect it.
		return Artifact{}, err
	}
	return s.repo.Get(ctx, a.ID)
}

func (s *Service) Open(ctx context.Context, artifactID string) (io.ReadCloser, Artifact, error) {
	a, err := s.Get(ctx, artifactID)
	if err != nil {
		return nil, Artifact{}, err
	}
	r, err := s.store.Open(ctx, a.StorageRef)
	if err != nil {
		return nil, Artifact{}, err
	}
	return r, a, nil
}

func (s *Service) VerifyContent(ctx context.Context, artifactID string) error {
	a, err := s.Get(ctx, artifactID)
	if err != nil {
		return err
	}
	return s.store.Verify(ctx, a.StorageRef, a.ContentHash, a.SizeBytes)
}
