package handlers

import "testing"

func TestGetPayloadOptionalInt(t *testing.T) {
	tests := []struct {
		name        string
		payload     map[string]interface{}
		key         string
		wantPresent bool
		wantNil     bool
		wantValue   int
		wantErr     string
	}{
		{
			name:        "missing field keeps absent state",
			payload:     map[string]interface{}{},
			key:         "battery_percent",
			wantPresent: false,
			wantNil:     true,
		},
		{
			name:        "explicit null is present and cleared",
			payload:     map[string]interface{}{"battery_percent": nil},
			key:         "battery_percent",
			wantPresent: true,
			wantNil:     true,
		},
		{
			name:        "numeric value parses",
			payload:     map[string]interface{}{"battery_percent": float64(80)},
			key:         "battery_percent",
			wantPresent: true,
			wantValue:   80,
		},
		{
			name:        "string value parses",
			payload:     map[string]interface{}{"battery_percent": "42"},
			key:         "battery_percent",
			wantPresent: true,
			wantValue:   42,
		},
		{
			name:    "invalid value returns error",
			payload: map[string]interface{}{"battery_percent": true},
			key:     "battery_percent",
			wantErr: "battery_percent must be a number",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getPayloadOptionalInt(tt.payload, tt.key)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("getPayloadOptionalInt() error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("getPayloadOptionalInt() unexpected error = %v", err)
			}
			if got.Present != tt.wantPresent {
				t.Fatalf("getPayloadOptionalInt() present = %v, want %v", got.Present, tt.wantPresent)
			}
			if tt.wantNil {
				if got.Value != nil {
					t.Fatalf("getPayloadOptionalInt() value = %v, want nil", *got.Value)
				}
				return
			}
			if got.Value == nil || *got.Value != tt.wantValue {
				t.Fatalf("getPayloadOptionalInt() value = %v, want %d", got.Value, tt.wantValue)
			}
		})
	}
}

func TestGetPayloadOptionalBool(t *testing.T) {
	tests := []struct {
		name        string
		payload     map[string]interface{}
		key         string
		wantPresent bool
		wantNil     bool
		wantValue   bool
		wantErr     string
	}{
		{
			name:        "missing field keeps absent state",
			payload:     map[string]interface{}{},
			key:         "is_in_flight",
			wantPresent: false,
			wantNil:     true,
		},
		{
			name:        "explicit null is present and cleared",
			payload:     map[string]interface{}{"is_in_flight": nil},
			key:         "is_in_flight",
			wantPresent: true,
			wantNil:     true,
		},
		{
			name:        "bool value parses",
			payload:     map[string]interface{}{"is_in_flight": true},
			key:         "is_in_flight",
			wantPresent: true,
			wantValue:   true,
		},
		{
			name:        "string value parses",
			payload:     map[string]interface{}{"is_in_flight": "false"},
			key:         "is_in_flight",
			wantPresent: true,
			wantValue:   false,
		},
		{
			name:    "invalid value returns error",
			payload: map[string]interface{}{"is_in_flight": 1},
			key:     "is_in_flight",
			wantErr: "is_in_flight must be true or false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getPayloadOptionalBool(tt.payload, tt.key)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("getPayloadOptionalBool() error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("getPayloadOptionalBool() unexpected error = %v", err)
			}
			if got.Present != tt.wantPresent {
				t.Fatalf("getPayloadOptionalBool() present = %v, want %v", got.Present, tt.wantPresent)
			}
			if tt.wantNil {
				if got.Value != nil {
					t.Fatalf("getPayloadOptionalBool() value = %v, want nil", *got.Value)
				}
				return
			}
			if got.Value == nil || *got.Value != tt.wantValue {
				t.Fatalf("getPayloadOptionalBool() value = %v, want %v", got.Value, tt.wantValue)
			}
		})
	}
}
