package resilience

import (
	"testing"
)

func TestFormatFloat(t *testing.T) {
	tests := []struct {
		input    float64
		expected string
	}{
		{1.0, "1.00"},
		{0.95, "0.95"},
		{0.9, "0.90"},
		{0.8, "0.80"},
		{0.7, "0.70"},
		{0.5, "0.50"},
		{0.0, "0.00"},
		{0.123456, "0.12"},
		{1.999, "2.00"},
		{-0.5, "-0.50"},
		{123.456, "123.46"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := formatFloat(tt.input)
			if result != tt.expected {
				t.Errorf("formatFloat(%f) = %s; want %s", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFormatInt64(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0"},
		{1, "1"},
		{-1, "-1"},
		{123456789, "123456789"},
		{-123456789, "-123456789"},
		{9223372036854775807, "9223372036854775807"},   // Max int64
		{-9223372036854775808, "-9223372036854775808"}, // Min int64
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := formatInt64(tt.input)
			if result != tt.expected {
				t.Errorf("formatInt64(%d) = %s; want %s", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFormatInt(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{1, "1"},
		{-1, "-1"},
		{8080, "8080"},
		{65535, "65535"},
		{-8080, "-8080"},
		{2147483647, "2147483647"},   // Max int32
		{-2147483648, "-2147483648"}, // Min int32
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := formatInt(tt.input)
			if result != tt.expected {
				t.Errorf("formatInt(%d) = %s; want %s", tt.input, result, tt.expected)
			}
		})
	}
}

func BenchmarkFormatFloat(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = formatFloat(0.95)
	}
}

func BenchmarkFormatInt64(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = formatInt64(123456789)
	}
}

func BenchmarkFormatInt(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = formatInt(8080)
	}
}
