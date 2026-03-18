package handlers

import (
	"testing"

	"xflight-backend/internal/auth"
)

func TestBuildCurrentMissionResponseNoActiveMission(t *testing.T) {
	dockingID := 5

	got := buildCurrentMissionResponse(auth.DeviceScopeDocking, 2, &dockingID, currentMissionLookupResult{})

	if got.ScopeType != auth.DeviceScopeDocking {
		t.Fatalf("ScopeType = %q, want %q", got.ScopeType, auth.DeviceScopeDocking)
	}
	if got.UavID != 2 {
		t.Fatalf("UavID = %d, want %d", got.UavID, 2)
	}
	if got.DockingID == nil || *got.DockingID != dockingID {
		t.Fatalf("DockingID = %v, want %d", got.DockingID, dockingID)
	}
	if got.HasActiveMission {
		t.Fatalf("HasActiveMission = true, want false")
	}
	if got.MissionID != nil || got.HistoryID != nil || got.MissionHistoryID != nil || got.Status != nil {
		t.Fatalf("expected mission fields to be nil when no active mission, got %+v", got)
	}
}

func TestBuildCurrentMissionResponseWithActiveMission(t *testing.T) {
	tokenDockingID := 5
	resultDockingID := 7

	got := buildCurrentMissionResponse(auth.DeviceScopeUav, 2, &tokenDockingID, currentMissionLookupResult{
		HistoryID: 456,
		MissionID: 123,
		DockingID: &resultDockingID,
		Status:    missionHistoryStatusTakeoff,
		Found:     true,
	})

	if !got.HasActiveMission {
		t.Fatalf("HasActiveMission = false, want true")
	}
	if got.MissionID == nil || *got.MissionID != 123 {
		t.Fatalf("MissionID = %v, want %d", got.MissionID, 123)
	}
	if got.HistoryID == nil || *got.HistoryID != 456 {
		t.Fatalf("HistoryID = %v, want %d", got.HistoryID, 456)
	}
	if got.MissionHistoryID == nil || *got.MissionHistoryID != 456 {
		t.Fatalf("MissionHistoryID = %v, want %d", got.MissionHistoryID, 456)
	}
	if got.Status == nil || *got.Status != missionHistoryStatusTakeoff {
		t.Fatalf("Status = %v, want %q", got.Status, missionHistoryStatusTakeoff)
	}
	if got.DockingID == nil || *got.DockingID != resultDockingID {
		t.Fatalf("DockingID = %v, want %d", got.DockingID, resultDockingID)
	}
}
