package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration is a time.Duration that unmarshals from Go duration strings ("1s", "500ms").
type Duration time.Duration

// UnmarshalYAML parses a duration string.
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(strings.TrimSpace(s))
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// MarshalYAML renders the duration back to a string.
func (d Duration) MarshalYAML() (any, error) { return time.Duration(d).String(), nil }

// Std returns the underlying time.Duration.
func (d Duration) Std() time.Duration { return time.Duration(d) }

// ByteSize is a byte count that unmarshals from strings like "64MB", "512KB", "1GB",
// or a plain integer number of bytes.
type ByteSize int64

// UnmarshalYAML parses a human-friendly byte size.
func (b *ByteSize) UnmarshalYAML(value *yaml.Node) error {
	// Allow a bare integer as well as a quoted size string.
	var raw string
	if err := value.Decode(&raw); err != nil {
		var n int64
		if err2 := value.Decode(&n); err2 == nil {
			*b = ByteSize(n)
			return nil
		}
		return err
	}
	n, err := parseByteSize(raw)
	if err != nil {
		return err
	}
	*b = ByteSize(n)
	return nil
}

// Bytes returns the size as an int64.
func (b ByteSize) Bytes() int64 { return int64(b) }

func parseByteSize(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" {
		return 0, fmt.Errorf("empty byte size")
	}
	units := []struct {
		suffix string
		mult   int64
	}{
		{"GB", 1 << 30}, {"GIB", 1 << 30},
		{"MB", 1 << 20}, {"MIB", 1 << 20},
		{"KB", 1 << 10}, {"KIB", 1 << 10},
		{"B", 1},
	}
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			num := strings.TrimSpace(strings.TrimSuffix(s, u.suffix))
			f, err := strconv.ParseFloat(num, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid byte size %q: %w", s, err)
			}
			return int64(f * float64(u.mult)), nil
		}
	}
	// Plain number of bytes.
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid byte size %q: %w", s, err)
	}
	return n, nil
}
