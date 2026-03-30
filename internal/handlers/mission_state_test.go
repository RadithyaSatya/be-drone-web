package handlers

import "testing"

func TestNormalizeMissionHistoryStatus(t *testing.T) {
	tests := map[string]string{
		"PreparingDock":  missionHistoryStatusPreparingDock,
		"preparing_dock": missionHistoryStatusPreparingDock,
		"safe-to-fly":    missionHistoryStatusSafeToFly,
		"SAFE TO FLY":    missionHistoryStatusSafeToFly,
		"takeoff":        missionHistoryStatusTakeoff,
		"landed":         missionHistoryStatusLanded,
		"DockConfirmed":  missionHistoryStatusDockConfirmed,
		"completed":      missionHistoryStatusCompleted,
		"failed":         missionHistoryStatusFailed,
		"aborted":        missionHistoryStatusAborted,
		"unknown":        "",
	}

	for input, want := range tests {
		if got := normalizeMissionHistoryStatus(input); got != want {
			t.Fatalf("normalizeMissionHistoryStatus(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestCanTransitionMissionHistoryStatus(t *testing.T) {
	tests := []struct {
		from string
		to   string
		want bool
	}{
		{missionHistoryStatusPreparingDock, missionHistoryStatusSafeToFly, true},
		{missionHistoryStatusSafeToFly, missionHistoryStatusTakeoff, true},
		{missionHistoryStatusTakeoff, missionHistoryStatusLanded, true},
		{missionHistoryStatusLanded, missionHistoryStatusDockConfirmed, true},
		{missionHistoryStatusDockConfirmed, missionHistoryStatusCompleted, true},
		{missionHistoryStatusPreparingDock, missionHistoryStatusFailed, true},
		{missionHistoryStatusSafeToFly, missionHistoryStatusAborted, true},
		{missionHistoryStatusPreparingDock, missionHistoryStatusTakeoff, false},
		{missionHistoryStatusTakeoff, missionHistoryStatusDockConfirmed, false},
		{missionHistoryStatusTakeoff, missionHistoryStatusCompleted, false},
		{missionHistoryStatusCompleted, missionHistoryStatusFailed, false},
	}

	for _, tt := range tests {
		if got := canTransitionMissionHistoryStatus(tt.from, tt.to); got != tt.want {
			t.Fatalf("canTransitionMissionHistoryStatus(%q, %q) = %v, want %v", tt.from, tt.to, got, tt.want)
		}
	}
}
