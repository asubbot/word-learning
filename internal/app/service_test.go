package app

import "testing"

func TestNormalizeLanguageCode(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "valid lowercase", input: "en", want: "EN"},
		{name: "valid with spaces", input: " ru ", want: "RU"},
		{name: "invalid with digit", input: "e1", wantErr: true},
		{name: "invalid too short", input: "e", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeLanguageCode(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseCardStatus(t *testing.T) {
	valid := []string{"active", "removed", " ACTIVE "}
	for _, value := range valid {
		if _, err := parseCardStatus(value); err != nil {
			t.Fatalf("value %q should be valid: %v", value, err)
		}
	}

	if _, err := parseCardStatus("unknown"); err == nil {
		t.Fatal("expected error for invalid status")
	}
}
