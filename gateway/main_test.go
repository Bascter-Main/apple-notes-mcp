package main

import "testing"

func TestValidateEndpoints(t *testing.T) {
	tests := []struct {
		name     string
		issuer   string
		audience string
		wantErr  bool
	}{
		{"valid", "https://mcp.example.com", "https://mcp.example.com/mcp", false},
		{"http", "http://mcp.example.com", "http://mcp.example.com/mcp", true},
		{"path", "https://mcp.example.com/oauth", "https://mcp.example.com/oauth/mcp", true},
		{"credentials", "https://user@mcp.example.com", "https://user@mcp.example.com/mcp", true},
		{"wrong audience", "https://mcp.example.com", "https://mcp.example.com/other", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEndpoints(tt.issuer, tt.audience)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateEndpoints() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
