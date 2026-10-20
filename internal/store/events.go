package store

import (
	"log/slog"

	"github.com/deverity/cowflow/internal/domain"
)

// EventSink receives the event that accompanies a workspace state change.
// Emit runs inside the state-change transaction: an error rolls the change
// back. It must not call back into the Store.
//
// PENDING U-DATA-1: until the events/outbox tables exist (S2-E2) the default
// sink only logs, so the event is not yet atomic with the state change.
type EventSink interface {
	Emit(ev domain.WorkspaceEvent) error
}

type logSink struct{}

func (logSink) Emit(ev domain.WorkspaceEvent) error {
	slog.Info("event", "type", ev.Type, "project_id", ev.ProjectID,
		"workspace_id", ev.WorkspaceID, "task_id", ev.TaskID, "payload", ev.Payload)
	return nil
}
