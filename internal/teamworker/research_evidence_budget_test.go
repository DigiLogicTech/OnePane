package teamworker

import (
	"context"
	"io"

	"github.com/DigiLogicTech/OnePane/internal/artifact"
)

type countedResearchEvidenceStore struct {
	artifact.BlobStore
	verifications, opens int
}

func (s *countedResearchEvidenceStore) Verify(ctx context.Context, ref, hash string, size int64) error {
	s.verifications++
	return s.BlobStore.Verify(ctx, ref, hash, size)
}
func (s *countedResearchEvidenceStore) Open(ctx context.Context, ref string) (io.ReadCloser, error) {
	s.opens++
	return s.BlobStore.Open(ctx, ref)
}
