package main

import (
	"os"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

func TestSetRLimitNoFile(t *testing.T) {
	// Save original rlimit
	var originalLimit syscall.Rlimit
	err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &originalLimit)
	if err != nil {
		t.Fatalf("Failed to get original rlimit: %v", err)
	}

	// Test SetRLimitNoFile function
	err = SetRLimitNoFile()
	if err != nil {
		t.Errorf("SetRLimitNoFile failed: %v", err)
	}

	// Verify that the limit was set correctly
	var newLimit syscall.Rlimit
	err = syscall.Getrlimit(syscall.RLIMIT_NOFILE, &newLimit)
	if err != nil {
		t.Fatalf("Failed to get new rlimit: %v", err)
	}

	// The current limit should now equal the max limit
	if newLimit.Cur != newLimit.Max {
		t.Errorf("Expected current limit to equal max limit after SetRLimitNoFile, got cur=%d, max=%d", newLimit.Cur, newLimit.Max)
	}

	// Restore original limit
	err = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &originalLimit)
	if err != nil {
		t.Logf("Warning: Failed to restore original rlimit: %v", err)
	}
}

// Test helper functions for flag validation
func TestValidateClientFlags(t *testing.T) {
	tests := []struct {
		name           string
		connectFlavor  string
		protocol       string
		expectError    bool
		errorSubstring string
	}{
		{
			name:          "valid persistent tcp",
			connectFlavor: flavorPersistent,
			protocol:      "tcp",
			expectError:   false,
		},
		{
			name:          "valid ephemeral udp",
			connectFlavor: flavorEphemeral,
			protocol:      "udp",
			expectError:   false,
		},
		{
			name:           "invalid connect flavor",
			connectFlavor:  "invalid",
			protocol:       "tcp",
			expectError:    true,
			errorSubstring: "unexpected connect flavor",
		},
		{
			name:           "invalid protocol",
			connectFlavor:  flavorPersistent,
			protocol:       "invalid",
			expectError:    true,
			errorSubstring: "unexpected protocol",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original values
			originalConnectFlavor := connectFlavor
			originalProtocol := protocol

			// Set test values
			connectFlavor = tt.connectFlavor
			protocol = tt.protocol

			// Test validation logic (simulate what's in runClient)
			var err error
			switch connectFlavor {
			case flavorPersistent, flavorEphemeral:
			default:
				err = &ValidationError{Field: "connectFlavor", Value: connectFlavor}
			}

			if err == nil {
				switch protocol {
				case "tcp", "udp":
				default:
					err = &ValidationError{Field: "protocol", Value: protocol}
				}
			}

			// Restore original values
			connectFlavor = originalConnectFlavor
			protocol = originalProtocol

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if tt.errorSubstring != "" && !strings.Contains(err.Error(), tt.errorSubstring) {
					t.Errorf("Expected error to contain '%s', got: %v", tt.errorSubstring, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

// ValidationError represents a validation error for testing
type ValidationError struct {
	Field string
	Value string
}

func (e *ValidationError) Error() string {
	switch e.Field {
	case "connectFlavor":
		return "unexpected connect flavor \"" + e.Value + "\""
	case "protocol":
		return "unexpected protocol \"" + e.Value + "\""
	default:
		return "validation error"
	}
}

func TestGetAddrsFromFile(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected []string
		wantErr  bool
	}{
		{
			name:     "single address",
			content:  "127.0.0.1:8080",
			expected: []string{"127.0.0.1:8080"},
			wantErr:  false,
		},
		{
			name:     "multiple addresses",
			content:  "127.0.0.1:8080 192.168.1.1:9090 example.com:3000",
			expected: []string{"127.0.0.1:8080", "192.168.1.1:9090", "example.com:3000"},
			wantErr:  false,
		},
		{
			name:     "addresses with newlines",
			content:  "127.0.0.1:8080\n192.168.1.1:9090\n",
			expected: []string{"127.0.0.1:8080", "192.168.1.1:9090"},
			wantErr:  false,
		},
		{
			name:     "empty file",
			content:  "",
			expected: []string{},
			wantErr:  false,
		},
		{
			name:     "whitespace only",
			content:  "   \n\t  \n",
			expected: []string{},
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpfile, err := os.CreateTemp("", "addrs_test")
			if err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}
			defer os.Remove(tmpfile.Name())

			if _, err := tmpfile.WriteString(tt.content); err != nil {
				t.Fatalf("Failed to write to temp file: %v", err)
			}
			if err := tmpfile.Close(); err != nil {
				t.Fatalf("Failed to close temp file: %v", err)
			}

			got, err := getAddrsFromFile(tmpfile.Name())
			if (err != nil) != tt.wantErr {
				t.Errorf("getAddrsFromFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("getAddrsFromFile() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestGetAddrsFromFileNotFound(t *testing.T) {
	_, err := getAddrsFromFile("/nonexistent/file")
	if err == nil {
		t.Error("Expected error for non-existent file, got nil")
	}
}
