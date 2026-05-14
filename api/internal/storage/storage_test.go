package storage

import (
	"errors"
	"testing"
	"time"
)

func TestValidateExpiry(t *testing.T) {
	tests := []struct {
		name         string
		dur          time.Duration
		wantErr      bool
		wantSentinel error
	}{
		{"negative", -1 * time.Second, true, nil},
		{"zero", 0, true, nil},
		{"one second", time.Second, false, nil},
		{"at max boundary", MaxSignedURLExpiry, false, nil},
		{"over max by one second", MaxSignedURLExpiry + time.Second, true, ErrExpiryTooLong},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateExpiry(tc.dur)
			if tc.wantErr && err == nil {
				t.Fatalf("validateExpiry(%v) = nil, want error", tc.dur)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("validateExpiry(%v) = %v, want nil", tc.dur, err)
			}
			if tc.wantSentinel != nil && !errors.Is(err, tc.wantSentinel) {
				t.Fatalf("validateExpiry(%v) = %v, want errors.Is(%v)", tc.dur, err, tc.wantSentinel)
			}
		})
	}
}

func TestNew(t *testing.T) {
	validR2 := Config{
		Provider:  "r2",
		Bucket:    "test-bucket",
		Endpoint:  "https://example.r2.cloudflarestorage.com",
		AccessKey: "ak",
		SecretKey: "sk",
	}
	validS3 := Config{
		Provider:  "s3",
		Bucket:    "test-bucket",
		Region:    "us-east-1",
		AccessKey: "ak",
		SecretKey: "sk",
	}

	override := func(base Config, mutate func(*Config)) Config {
		c := base
		mutate(&c)
		return c
	}

	tests := []struct {
		name         string
		cfg          Config
		wantErr      bool
		wantSentinel error
	}{
		{
			name: "empty provider defaults to r2",
			cfg:  override(validR2, func(c *Config) { c.Provider = "" }),
		},
		{
			name: "r2 with all required",
			cfg:  validR2,
		},
		{
			name:    "r2 missing endpoint",
			cfg:     override(validR2, func(c *Config) { c.Endpoint = "" }),
			wantErr: true,
		},
		{
			name:    "r2 missing bucket",
			cfg:     override(validR2, func(c *Config) { c.Bucket = "" }),
			wantErr: true,
		},
		{
			name:    "r2 missing access key",
			cfg:     override(validR2, func(c *Config) { c.AccessKey = "" }),
			wantErr: true,
		},
		{
			name:    "r2 missing secret key",
			cfg:     override(validR2, func(c *Config) { c.SecretKey = "" }),
			wantErr: true,
		},
		{
			name: "s3 with all required",
			cfg:  validS3,
		},
		{
			name:    "s3 missing region",
			cfg:     override(validS3, func(c *Config) { c.Region = "" }),
			wantErr: true,
		},
		{
			name:    "s3 missing creds",
			cfg:     override(validS3, func(c *Config) { c.AccessKey = ""; c.SecretKey = "" }),
			wantErr: true,
		},
		{
			name:         "gcs not yet supported",
			cfg:          Config{Provider: "gcs"},
			wantErr:      true,
			wantSentinel: ErrUnknownProvider,
		},
		{
			name:         "garbage provider",
			cfg:          Config{Provider: "not-a-thing"},
			wantErr:      true,
			wantSentinel: ErrUnknownProvider,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := New(tc.cfg)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("New(%+v) = _, nil, want error", tc.cfg)
				}
				if tc.wantSentinel != nil && !errors.Is(err, tc.wantSentinel) {
					t.Fatalf("New(%+v) = _, %v, want errors.Is(%v)", tc.cfg, err, tc.wantSentinel)
				}
				return
			}
			if err != nil {
				t.Fatalf("New(%+v) = _, %v, want nil", tc.cfg, err)
			}
			if got == nil {
				t.Fatalf("New(%+v) = nil, _, want non-nil Storage", tc.cfg)
			}
		})
	}
}
