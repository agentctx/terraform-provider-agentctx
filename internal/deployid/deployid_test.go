package deployid

import (
	"regexp"
	"testing"
	"time"
)

// idPattern matches the full format: dep_<YYYYMMDD>T<HHmmss>Z_<8hex>
var idPattern = regexp.MustCompile(`^dep_\d{8}T\d{6}Z_[0-9a-f]{8}$`)

func TestNew(t *testing.T) {
	t.Run("valid format", func(t *testing.T) {
		id := New()
		if !idPattern.MatchString(id) {
			t.Fatalf("New() = %q, does not match expected pattern dep_<timestamp>_<8hex>", id)
		}
	})

	t.Run("timestamp is close to now", func(t *testing.T) {
		before := time.Now().UTC()
		id := New()
		after := time.Now().UTC()

		ts, err := Parse(id)
		if err != nil {
			t.Fatalf("Parse(%q) returned error: %v", id, err)
		}

		if ts.Before(before.Truncate(time.Second)) {
			t.Errorf("parsed time %v is before test start %v", ts, before)
		}
		// after + 1s to account for truncation to seconds
		if ts.After(after.Add(time.Second)) {
			t.Errorf("parsed time %v is after test end %v", ts, after)
		}
	})

	t.Run("unique IDs", func(t *testing.T) {
		seen := make(map[string]bool)
		for i := 0; i < 100; i++ {
			id := New()
			if seen[id] {
				t.Fatalf("duplicate ID generated: %q", id)
			}
			seen[id] = true
		}
	})
}

func TestParse(t *testing.T) {
	t.Run("valid ID", func(t *testing.T) {
		id := "dep_20260213T200102Z_6f2c9a1b"
		ts, err := Parse(id)
		if err != nil {
			t.Fatalf("Parse(%q) returned error: %v", id, err)
		}

		want := time.Date(2026, 2, 13, 20, 1, 2, 0, time.UTC)
		if !ts.Equal(want) {
			t.Errorf("Parse(%q) = %v, want %v", id, ts, want)
		}
	})

	t.Run("wrong prefix", func(t *testing.T) {
		_, err := Parse("xyz_20260213T200102Z_6f2c9a1b")
		if err == nil {
			t.Fatal("expected error for wrong prefix, got nil")
		}
	})

	t.Run("bad timestamp", func(t *testing.T) {
		_, err := Parse("dep_not-a-timestamp_6f2c9a1b")
		if err == nil {
			t.Fatal("expected error for bad timestamp, got nil")
		}
	})

	t.Run("missing random segment", func(t *testing.T) {
		_, err := Parse("dep_20260213T200102Z")
		if err == nil {
			t.Fatal("expected error for missing random segment, got nil")
		}
	})

	t.Run("short random segment", func(t *testing.T) {
		_, err := Parse("dep_20260213T200102Z_6f2c")
		if err == nil {
			t.Fatal("expected error for short random segment, got nil")
		}
	})

	t.Run("non-hex random segment", func(t *testing.T) {
		_, err := Parse("dep_20260213T200102Z_zzzzzzzz")
		if err == nil {
			t.Fatal("expected error for non-hex random segment, got nil")
		}
	})

	t.Run("empty string", func(t *testing.T) {
		_, err := Parse("")
		if err == nil {
			t.Fatal("expected error for empty string, got nil")
		}
	})
}

func TestIsValid(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want bool
	}{
		{
			name: "valid ID",
			id:   "dep_20260213T200102Z_6f2c9a1b",
			want: true,
		},
		{
			name: "generated ID",
			id:   New(),
			want: true,
		},
		{
			name: "wrong prefix",
			id:   "abc_20260213T200102Z_6f2c9a1b",
			want: false,
		},
		{
			name: "bad timestamp",
			id:   "dep_badtimestamp_6f2c9a1b",
			want: false,
		},
		{
			name: "short random",
			id:   "dep_20260213T200102Z_6f2c",
			want: false,
		},
		{
			name: "non-hex random",
			id:   "dep_20260213T200102Z_ghijklmn",
			want: false,
		},
		{
			name: "empty string",
			id:   "",
			want: false,
		},
		{
			name: "missing underscore separator",
			id:   "dep_20260213T200102Z6f2c9a1b",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsValid(tt.id)
			if got != tt.want {
				t.Errorf("IsValid(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}
