package runner

import (
	"github.com/tsosunchia/iNetSpeed-CLI/internal/endpoint"
	"testing"
)

func TestFormatLocation(t *testing.T) {
	tests := []struct {
		name string
		info endpoint.IPInfo
		want string
	}{
		{"empty", endpoint.IPInfo{}, "?"},
		{"city_only", endpoint.IPInfo{City: "Tokyo"}, "Tokyo"},
		{"full", endpoint.IPInfo{City: "Tokyo", RegionName: "Kanto", Country: "Japan"}, "Tokyo, Kanto, Japan"},
		{"city_eq_region", endpoint.IPInfo{City: "Tokyo", RegionName: "Tokyo", Country: "Japan"}, "Tokyo, Japan"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatLocation(tt.info)
			if got != tt.want {
				t.Errorf("formatLocation() = %q, want %q", got, tt.want)
			}
		})
	}
}
