// Package config: unit tests for port parsing.
// Package config: 端口解析单元测试。
package config

import (
	"reflect"
	"testing"
)

func TestParsePortSpec(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		want    []int
		wantErr bool
	}{
		{
			name: "single port",
			spec: "80",
			want: []int{80},
		},
		{
			name: "multiple ports",
			spec: "22,80,443",
			want: []int{22, 80, 443},
		},
		{
			name: "port range",
			spec: "8000-8005",
			want: []int{8000, 8001, 8002, 8003, 8004, 8005},
		},
		{
			name: "port group - common",
			spec: "common",
			want: CommonPorts,
		},
		{
			name: "port group - db",
			spec: "db",
			want: DbPorts,
		},
		{
			name: "mixed format",
			spec: "22,80-82,443",
			want: []int{22, 80, 81, 82, 443},
		},
		{
			name: "deduplication",
			spec: "80,80,80",
			want: []int{80},
		},
		{
			name:    "empty spec",
			spec:    "",
			wantErr: true,
		},
		{
			name:    "invalid port",
			spec:    "99999",
			wantErr: true,
		},
		{
			name:    "invalid range",
			spec:    "100-50",
			wantErr: true,
		},
		{
			name:    "non-numeric",
			spec:    "abc",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePortSpec(tt.spec)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParsePortSpec(%q) error = %v, wantErr %v", tt.spec, err, tt.wantErr)
				return
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParsePortSpec(%q) = %v, want %v", tt.spec, got, tt.want)
			}
		})
	}
}

func TestParsePortSpecAll(t *testing.T) {
	ports, err := ParsePortSpec("all")
	if err != nil {
		t.Fatalf("ParsePortSpec(\"all\") error = %v", err)
	}
	if len(ports) != 65535 {
		t.Errorf("ParsePortSpec(\"all\") returned %d ports, want 65535", len(ports))
	}
	if ports[0] != 1 || ports[len(ports)-1] != 65535 {
		t.Errorf("ParsePortSpec(\"all\") range incorrect: [%d, %d], want [1, 65535]", ports[0], ports[len(ports)-1])
	}
}

func TestGetPortGroup(t *testing.T) {
	tests := []struct {
		name      string
		groupName string
		wantOK    bool
		wantLen   int
	}{
		{"main group", "main", true, len(MainPorts)},
		{"web group", "web", true, len(WebPorts)},
		{"db group", "db", true, len(DbPorts)},
		{"service group", "service", true, len(ServicePorts)},
		{"common group", "common", true, len(CommonPorts)},
		{"invalid group", "invalid", false, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ports, ok := GetPortGroup(tt.groupName)
			if ok != tt.wantOK {
				t.Errorf("GetPortGroup(%q) ok = %v, want %v", tt.groupName, ok, tt.wantOK)
			}
			if ok && len(ports) != tt.wantLen {
				t.Errorf("GetPortGroup(%q) returned %d ports, want %d", tt.groupName, len(ports), tt.wantLen)
			}
		})
	}
}

func TestParsePortRange(t *testing.T) {
	tests := []struct {
		name      string
		rangeSpec string
		want      []int
		wantErr   bool
	}{
		{
			name:      "valid range",
			rangeSpec: "80-85",
			want:      []int{80, 81, 82, 83, 84, 85},
		},
		{
			name:      "single port range",
			rangeSpec: "80-80",
			want:      []int{80},
		},
		{
			name:      "invalid format",
			rangeSpec: "80",
			wantErr:   true,
		},
		{
			name:      "reversed range",
			rangeSpec: "85-80",
			wantErr:   true,
		},
		{
			name:      "too large range",
			rangeSpec: "1-20000",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePortRange(tt.rangeSpec)
			if (err != nil) != tt.wantErr {
				t.Errorf("parsePortRange(%q) error = %v, wantErr %v", tt.rangeSpec, err, tt.wantErr)
				return
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parsePortRange(%q) = %v, want %v", tt.rangeSpec, got, tt.want)
			}
		})
	}
}

func BenchmarkParsePortSpec(b *testing.B) {
	specs := []string{
		"22,80,443",
		"8000-8100",
		"web",
		"db,3306,5432",
	}

	for _, spec := range specs {
		b.Run(spec, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_, _ = ParsePortSpec(spec)
			}
		})
	}
}
