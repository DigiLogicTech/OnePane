package budget

import "errors"

type ReservationStatus string

const (
	ReservationReserved  ReservationStatus = "reserved"
	ReservationCommitted ReservationStatus = "committed"
	ReservationReleased  ReservationStatus = "released"
	ReservationExpired   ReservationStatus = "expired"
)

type Account struct {
	ID              string
	WorkspaceID     *string
	ParentAccountID *string
	Name            string
	Unit            string
	LimitAmount     int64
	CommittedAmount int64
	ReservedAmount  int64
	PeriodJSON      []byte
	Revision        int64
	CreatedAt       int64
	UpdatedAt       int64
}

func (a Account) Available() int64 { return a.LimitAmount - a.CommittedAmount - a.ReservedAmount }

type Reservation struct {
	ID                       string
	BudgetAccountID          string
	TaskID                   *string
	InferenceRequestID       *string
	AgentRuntimeInvocationID *string
	Amount                   int64
	Status                   ReservationStatus
	ActualAmount             *int64
	ReservedAt               int64
	ExpiresAt                int64
	ClosedAt                 *int64
}

type CreateAccountCommand struct {
	WorkspaceID      *string
	ParentAccountID  *string
	Name             string
	Unit             string
	LimitAmount      int64
	PeriodJSON       []byte
	ActorPrincipalID *string
	RequestID        *string
	TraceID          *string
}

type ReserveCommand struct {
	WorkspaceID      string
	AccountID        string
	TaskID           *string
	Amount           int64
	TTLMillis        int64
	ActorPrincipalID *string
	RequestID        *string
	TraceID          *string
}

type CloseCommand struct {
	ReservationID    string
	ActualAmount     *int64
	ActorPrincipalID *string
	RequestID        *string
	TraceID          *string
}

var (
	ErrInvalidCommand          = errors.New("invalid budget command")
	ErrAccountNotFound         = errors.New("budget account not found")
	ErrReservationNotFound     = errors.New("budget reservation not found")
	ErrInsufficientBudget      = errors.New("insufficient budget")
	ErrReservationClosed       = errors.New("budget reservation is closed")
	ErrReservationActive       = errors.New("budget reservation is attached to active work")
	ErrReservationExpired      = errors.New("budget reservation expired")
	ErrWorkspaceMismatch       = errors.New("budget workspace mismatch")
	ErrTaskMismatch            = errors.New("budget task mismatch")
	ErrInferenceAlreadyBound   = errors.New("budget reservation already bound to inference")
	ErrReservationAlreadyBound = errors.New("budget reservation already bound to another request")
	ErrActualExceedsReserve    = errors.New("actual budget usage exceeds reservation")
)
