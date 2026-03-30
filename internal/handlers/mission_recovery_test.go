package handlers

import (
	"testing"
	"time"
)

func TestDecideMissionAutoRecovery(t *testing.T) {
	now := time.Date(2026, 3, 14, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name         string
		status       string
		lastActivity time.Time
		wantStatus   string
		wantRecover  bool
	}{
		{
			name:         "preparing dock times out to aborted",
			status:       missionHistoryStatusPreparingDock,
			lastActivity: now.Add(-missionRecoveryPreparingDockMaxAge),
			wantStatus:   missionHistoryStatusAborted,
			wantRecover:  true,
		},
		{
			name:         "safe to fly before timeout stays active",
			status:       missionHistoryStatusSafeToFly,
			lastActivity: now.Add(-missionRecoverySafeToFlyMaxAge + time.Second),
			wantRecover:  false,
		},
		{
			name:         "takeoff times out to failed",
			status:       missionHistoryStatusTakeoff,
			lastActivity: now.Add(-missionRecoveryTakeoffMaxAge - time.Second),
			wantStatus:   missionHistoryStatusFailed,
			wantRecover:  true,
		},
		{
			name:         "landed times out to failed",
			status:       missionHistoryStatusLanded,
			lastActivity: now.Add(-missionRecoveryLandedMaxAge - time.Second),
			wantStatus:   missionHistoryStatusFailed,
			wantRecover:  true,
		},
		{
			name:         "dock confirmed times out to failed",
			status:       missionHistoryStatusDockConfirmed,
			lastActivity: now.Add(-missionRecoveryDockConfirmedMaxAge - time.Second),
			wantStatus:   missionHistoryStatusFailed,
			wantRecover:  true,
		},
		{
			name:         "terminal states are ignored",
			status:       missionHistoryStatusCompleted,
			lastActivity: now.Add(-time.Hour),
			wantRecover:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := decideMissionAutoRecovery(tt.status, tt.lastActivity, now)
			if ok != tt.wantRecover {
				t.Fatalf("decideMissionAutoRecovery() recover = %v, want %v", ok, tt.wantRecover)
			}
			if !tt.wantRecover {
				return
			}
			if got.TargetStatus != tt.wantStatus {
				t.Fatalf("decideMissionAutoRecovery() status = %q, want %q", got.TargetStatus, tt.wantStatus)
			}
			if got.FailureCode != missionRecoveryFailureCode {
				t.Fatalf("decideMissionAutoRecovery() failure code = %q, want %q", got.FailureCode, missionRecoveryFailureCode)
			}
		})
	}
}

func TestMissionStatusAfterAutoRecovery(t *testing.T) {
	now := time.Date(2026, 3, 14, 12, 0, 0, 0, time.UTC)

	t.Run("non recurring keeps terminal status", func(t *testing.T) {
		status, next := missionStatusAfterAutoRecovery(missionRecoveryCandidate{}, missionHistoryStatusFailed, now)
		if status != missionHistoryStatusFailed || next != "" {
			t.Fatalf("missionStatusAfterAutoRecovery() = (%q, %q), want (%q, \"\")", status, next, missionHistoryStatusFailed)
		}
	})

	t.Run("recurring mission returns to waiting with next schedule", func(t *testing.T) {
		unit := recurrenceUnitHour
		interval := 4
		status, next := missionStatusAfterAutoRecovery(missionRecoveryCandidate{
			IsRecurring:        true,
			Schedule:           "2026-03-14T08:00:00Z",
			RecurrenceUnit:     &unit,
			RecurrenceInterval: &interval,
		}, missionHistoryStatusFailed, now)
		if status != "Waiting" {
			t.Fatalf("missionStatusAfterAutoRecovery() status = %q, want %q", status, "Waiting")
		}
		if next != "2026-03-14T16:00:00Z" {
			t.Fatalf("missionStatusAfterAutoRecovery() next schedule = %q, want %q", next, "2026-03-14T16:00:00Z")
		}
	})
}

func TestResolveMissionTemplateAfterTerminalState(t *testing.T) {
	now := time.Date(2026, 3, 14, 12, 0, 0, 0, time.UTC)

	t.Run("non recurring failed stays failed", func(t *testing.T) {
		status, next := resolveMissionTemplateAfterTerminalState(false, "2026-03-14T08:00:00Z", nil, nil, missionHistoryStatusFailed, now)
		if status != missionHistoryStatusFailed || next != "" {
			t.Fatalf("resolveMissionTemplateAfterTerminalState() = (%q, %q), want (%q, \"\")", status, next, missionHistoryStatusFailed)
		}
	})

	t.Run("recurring failed returns waiting and next schedule", func(t *testing.T) {
		unit := recurrenceUnitHour
		interval := 4
		status, next := resolveMissionTemplateAfterTerminalState(true, "2026-03-14T08:00:00Z", &unit, &interval, missionHistoryStatusFailed, now)
		if status != "Waiting" {
			t.Fatalf("resolveMissionTemplateAfterTerminalState() status = %q, want %q", status, "Waiting")
		}
		if next != "2026-03-14T16:00:00Z" {
			t.Fatalf("resolveMissionTemplateAfterTerminalState() next schedule = %q, want %q", next, "2026-03-14T16:00:00Z")
		}
	})
}
