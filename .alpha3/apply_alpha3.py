#!/usr/bin/env python3
from pathlib import Path

ROOT = Path("buildsrc")

def replace(rel, old, new):
    p = ROOT / rel
    s = p.read_text(encoding="utf-8")
    if old not in s:
        raise SystemExit(f"Alpha 3 transform anchor missing: {rel}: {old[:80]!r}")
    p.write_text(s.replace(old, new, 1), encoding="utf-8")

def insert_after(rel, anchor, addition):
    replace(rel, anchor, anchor + addition)

# Storage location is independent from application/model storage.
replace("internal/config/config.go",
'''type StorageConfig struct {
	DataDir string `yaml:"data_dir"`
}''',
'''type StorageConfig struct {
	DataDir     string `yaml:"data_dir"`
	ProjectRoot string `yaml:"project_root,omitempty"`
}''')
insert_after("internal/config/config.go",
'''	if strings.TrimSpace(cfg.Storage.DataDir) == "" {
		return fmt.Errorf("storage.data_dir must not be empty")
	}
''',
'''	if v := strings.TrimSpace(cfg.Storage.ProjectRoot); v != "" && !filepath.IsAbs(v) {
		return fmt.Errorf("storage.project_root must be an absolute path")
	}
''')

# Inference exposes a single global runtime scheduling hook.
insert_after("internal/inference/service.go",
'''	"github.com/DigiLogicTech/OnePane/internal/storage"
)
''',
'''
type RuntimeScheduleRequest struct {
	WorkspaceID string
	TaskID *string
	InferenceRequestID string
	Deployment ModelDeployment
	Priority string
}
type RuntimeScheduleLease interface {
	ReportBoundary(context.Context, string) error
	Release(context.Context, error)
}
type RuntimeCoordinator interface {
	Acquire(context.Context, RuntimeScheduleRequest) (RuntimeScheduleLease, error)
}
''')
replace("internal/inference/service.go",
'''	localNodeID string
}''',
'''	localNodeID string
	runtimeCoordinator RuntimeCoordinator
}''')
insert_after("internal/inference/service.go",
'''	s.localNodeID = strings.TrimSpace(localNodeID)
	return nil
}
''',
'''
func (s *Service) ConfigureRuntimeCoordinator(c RuntimeCoordinator) error {
	if c == nil { return fmt.Errorf("%w: runtime coordinator is required", ErrExecutionUnavailable) }
	s.runtimeCoordinator = c
	return nil
}
''')

# Project workspace service retains the DB and storage-root state required by Alpha 3.
replace("internal/projectworkspace/service.go", '"strings"\n', '"strings"\n\t"sync"\n')
replace("internal/projectworkspace/service.go",
'''type Service struct {
	repo   repository
	tx     storage.Transactor
	events event.Store
	outbox outbox.Store
	ids    id.Generator
	clock  clock.Clock
}''',
'''type Service struct {
	db *sql.DB
	repo repository
	tx storage.Transactor
	events event.Store
	outbox outbox.Store
	ids id.Generator
	clock clock.Clock
	projectRoot string
	storageMu sync.Mutex
}''')
replace("internal/projectworkspace/service.go",
'return &Service{repo: newSQLRepository(db), tx: tx, events: event.Store{}, outbox: outbox.Store{}, ids: id.Generator{}, clock: clk}',
'return &Service{db: db, repo: newSQLRepository(db), tx: tx, events: event.Store{}, outbox: outbox.Store{}, ids: id.Generator{}, clock: clk}')

print("Alpha 3 deterministic transforms applied")
