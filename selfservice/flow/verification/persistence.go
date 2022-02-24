package verification

import (
	"context"
	"time"

	"github.com/gofrs/uuid"
)

type (
	FlowPersistenceProvider interface {
		VerificationFlowPersister() FlowPersister
	}
	FlowPersister interface {
		CreateVerificationFlow(context.Context, *Flow) error
		GetVerificationFlow(ctx context.Context, id uuid.UUID) (*Flow, error)
		UpdateVerificationFlow(context.Context, *Flow) error
		DeleteExpiredVerificationFlows(context.Context, time.Time, int, int) error
	}
)
