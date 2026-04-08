package handlers

import "testing"

func TestIsDeviceScopedUavUpdateAllowed(t *testing.T) {
	tests := []struct {
		name string
		req  uavUpsertRequest
		want bool
	}{
		{
			name: "allows operational UAV fields",
			req: uavUpsertRequest{
				HomeLatitude:  floatPtr(-6.2),
				HomeLongitude: floatPtr(106.8),
				MaxRangeMeter: intPtr(5000),
			},
			want: true,
		},
		{
			name: "rejects admin UAV fields",
			req: uavUpsertRequest{
				Name:         stringPtr("Falcon"),
				HomeLatitude: floatPtr(-6.2),
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isDeviceScopedUavUpdateAllowed(tt.req); got != tt.want {
				t.Fatalf("isDeviceScopedUavUpdateAllowed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsDeviceScopedDockingUpdateAllowed(t *testing.T) {
	tests := []struct {
		name string
		req  dockingUpsertRequest
		want bool
	}{
		{
			name: "allows docking identity fields",
			req: dockingUpsertRequest{
				Name:         stringPtr("Dock A"),
				LocationName: stringPtr("Hangar 1"),
				Latitude:     floatPtr(-6.2),
				Longitude:    floatPtr(106.8),
			},
			want: true,
		},
		{
			name: "rejects docking ownership fields",
			req: dockingUpsertRequest{
				IsPrimary: boolPtr(true),
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isDeviceScopedDockingUpdateAllowed(tt.req); got != tt.want {
				t.Fatalf("isDeviceScopedDockingUpdateAllowed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func stringPtr(value string) *string {
	return &value
}

func intPtr(value int) *int {
	return &value
}

func floatPtr(value float64) *float64 {
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}
