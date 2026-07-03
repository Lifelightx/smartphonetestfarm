package goios

import (
	"reflect"
	"testing"
)

func TestParseIOSList(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []IOSDeviceEntry
		wantErr bool
	}{
		{
			name:  "Clean JSON array",
			input: `[{"udid": "12345", "name": "iPhone 15", "type": "iPhone16,1", "version": "17.4"}]`,
			want: []IOSDeviceEntry{
				{UDID: "12345", DeviceName: "iPhone 15", ProductType: "iPhone16,1", ProductVersion: "17.4"},
			},
			wantErr: false,
		},
		{
			name:  "Device list wrapper object",
			input: `{"deviceList": [{"udid": "12345", "name": "iPhone 15", "type": "iPhone16,1", "version": "17.4"}]}`,
			want: []IOSDeviceEntry{
				{UDID: "12345", DeviceName: "iPhone 15", ProductType: "iPhone16,1", ProductVersion: "17.4"},
			},
			wantErr: false,
		},
		{
			name:    "Device list wrapper object empty",
			input:   `{"deviceList": []}`,
			want:    []IOSDeviceEntry{},
			wantErr: false,
		},
		{
			name:  "Single device object",
			input: `{"udid": "12345", "name": "iPhone 15", "type": "iPhone16,1", "version": "17.4"}`,
			want: []IOSDeviceEntry{
				{UDID: "12345", DeviceName: "iPhone 15", ProductType: "iPhone16,1", ProductVersion: "17.4"},
			},
			wantErr: false,
		},
		{
			name: "Warning log and empty deviceList object",
			input: `{"time":"2026-07-02T14:01:17.036030911+05:30","level":"WARN","msg":"go-ios agent is not running. You might need to start it..."}
{"deviceList":[]}`,
			want:    []IOSDeviceEntry{},
			wantErr: false,
		},
		{
			name: "Warning log and deviceList object with strings",
			input: `{"time":"2026-07-02T14:01:17.036030911+05:30","level":"WARN","msg":"go-ios agent is not running. You might need to start it..."}
{"deviceList":["00008020-001A4D3E1402002E"]}`,
			want: []IOSDeviceEntry{
				{UDID: "00008020-001A4D3E1402002E"},
			},
			wantErr: false,
		},
		{
			name:  "JSON array of strings",
			input: `["00008020-001A4D3E1402002E"]`,
			want: []IOSDeviceEntry{
				{UDID: "00008020-001A4D3E1402002E"},
			},
			wantErr: false,
		},
		{
			name: "Warning log and non-empty deviceList object",
			input: `{"time":"2026-07-02T14:01:17.036030911+05:30","level":"WARN","msg":"go-ios agent is not running. You might need to start it..."}
{"deviceList":[{"udid": "12345", "name": "iPhone 15"}]}`,
			want: []IOSDeviceEntry{
				{UDID: "12345", DeviceName: "iPhone 15"},
			},
			wantErr: false,
		},
		{
			name: "Pretty printed JSON wrapper",
			input: `{
				"deviceList": [
					{
						"udid": "12345",
						"name": "iPhone 15"
					}
				]
			}`,
			want: []IOSDeviceEntry{
				{UDID: "12345", DeviceName: "iPhone 15"},
			},
			wantErr: false,
		},
		{
			name:    "Invalid format",
			input:   `not a json string`,
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseIOSList(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseIOSList() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseIOSList() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseJSONMap(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    map[string]interface{}
		wantErr bool
	}{
		{
			name:    "Simple map",
			input:   `{"width": 1170, "height": 2532}`,
			want:    map[string]interface{}{"width": float64(1170), "height": float64(2532)},
			wantErr: false,
		},
		{
			name: "Warning log and map",
			input: `{"time":"2026-07-02T14:01:17.036030911+05:30","level":"WARN","msg":"go-ios agent is not running."}
{"width": 1170, "height": 2532}`,
			want:    map[string]interface{}{"width": float64(1170), "height": float64(2532)},
			wantErr: false,
		},
		{
			name:    "Invalid map",
			input:   `[1, 2, 3]`,
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseJSONMap(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseJSONMap() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseJSONMap() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseIOSAppList(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantApps    []IOSAppInfo
		wantMapApps map[string]interface{}
		wantErr     bool
	}{
		{
			name:        "Clean app list array",
			input:       `[{"CFBundleIdentifier": "com.test.app", "CFBundleDisplayName": "Test App"}]`,
			wantApps:    []IOSAppInfo{{BundleID: "com.test.app", DisplayName: "Test App"}},
			wantMapApps: nil,
			wantErr:     false,
		},
		{
			name:        "Clean app list map",
			input:       `{"com.test.app": {}}`,
			wantApps:    nil,
			wantMapApps: map[string]interface{}{"com.test.app": map[string]interface{}{}},
			wantErr:     false,
		},
		{
			name: "Warning log and map",
			input: `{"time":"2026-07-02T14:01:17.036030911+05:30","level":"WARN","msg":"go-ios agent is not running."}
{"com.test.app": {}}`,
			wantApps:    nil,
			wantMapApps: map[string]interface{}{"com.test.app": map[string]interface{}{}},
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotApps, gotMapApps, err := parseIOSAppList(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseIOSAppList() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if !reflect.DeepEqual(gotApps, tt.wantApps) {
					t.Errorf("parseIOSAppList() gotApps = %v, want %v", gotApps, tt.wantApps)
				}
				if !reflect.DeepEqual(gotMapApps, tt.wantMapApps) {
					t.Errorf("parseIOSAppList() gotMapApps = %v, want %v", gotMapApps, tt.wantMapApps)
				}
			}
		})
	}
}
