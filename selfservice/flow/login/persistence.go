package login

import (
	"context"
	"time"

	"github.com/gofrs/uuid"
)

type (
	FlowPersister interface {
		UpdateLoginFlow(context.Context, *Flow) error
		CreateLoginFlow(context.Context, *Flow) error
		GetLoginFlow(context.Context, uuid.UUID) (*Flow, error)
		ForceLoginFlow(ctx context.Context, id uuid.UUID) error
		DeleteExpiredLoginFlows(context.Context, time.Time, int, int) error
	}
	FlowPersistenceProvider interface {
		LoginFlowPersister() FlowPersister
	}
)
