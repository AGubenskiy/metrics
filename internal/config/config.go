package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// DurationSeconds stores an application interval as number of seconds.
type DurationSeconds int

// ParseDurationSeconds accepts either Go duration strings like "1s" or integer seconds.
func ParseDurationSeconds(value string) (int, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, fmt.Errorf("value is empty")
	}

	if duration, err := time.ParseDuration(trimmed); err == nil {
		if duration%time.Second != 0 {
			return 0, fmt.Errorf("duration %q is not a whole number of seconds", trimmed)
		}
		return int(duration / time.Second), nil
	}

	seconds, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, fmt.Errorf("expected duration string or integer seconds: %w", err)
	}
	return seconds, nil
}

func (d *DurationSeconds) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		return nil
	}

	if len(trimmed) > 0 && trimmed[0] == '"' {
		var value string
		if err := json.Unmarshal(trimmed, &value); err != nil {
			return err
		}

		seconds, err := ParseDurationSeconds(value)
		if err != nil {
			return err
		}
		*d = DurationSeconds(seconds)
		return nil
	}

	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err != nil {
		return err
	}

	seconds, err := strconv.Atoi(number.String())
	if err != nil {
		return fmt.Errorf("expected integer seconds or duration string: %w", err)
	}
	*d = DurationSeconds(seconds)
	return nil
}

func (d DurationSeconds) Int() int {
	return int(d)
}

type Server struct {
	Address         *string          `json:"address"`
	Restore         *bool            `json:"restore"`
	StoreInterval   *DurationSeconds `json:"store_interval"`
	StoreFile       *string          `json:"store_file"`
	FileStoragePath *string          `json:"file_storage_path"`
	DatabaseDSN     *string          `json:"database_dsn"`
	Key             *string          `json:"key"`
	CryptoKey       *string          `json:"crypto_key"`
	TrustedSubnet   *string          `json:"trusted_subnet"`
	AuditFile       *string          `json:"audit_file"`
	AuditURL        *string          `json:"audit_url"`
}

func (c Server) StoreFileValue() *string {
	if c.StoreFile != nil {
		return c.StoreFile
	}
	return c.FileStoragePath
}

func (c Server) HasFileStorageSettings() bool {
	return c.Restore != nil ||
		c.StoreInterval != nil ||
		c.StoreFile != nil ||
		c.FileStoragePath != nil
}

type Agent struct {
	Address        *string          `json:"address"`
	ReportInterval *DurationSeconds `json:"report_interval"`
	PollInterval   *DurationSeconds `json:"poll_interval"`
	RateLimit      *int             `json:"rate_limit"`
	Key            *string          `json:"key"`
	CryptoKey      *string          `json:"crypto_key"`
}

func LoadServer(path string) (Server, error) {
	var cfg Server
	if err := loadJSON(path, &cfg); err != nil {
		return Server{}, err
	}
	return cfg, nil
}

func LoadAgent(path string) (Agent, error) {
	var cfg Agent
	if err := loadJSON(path, &cfg); err != nil {
		return Agent{}, err
	}
	return cfg, nil
}

func ResolveString(envNames []string, flagSet bool, flagValue string, configValue *string, defaultValue string) string {
	if _, value, ok := lookupNonEmptyEnv(envNames...); ok {
		return value
	}

	if flagSet {
		trimmed := strings.TrimSpace(flagValue)
		if trimmed != "" {
			return trimmed
		}
		return defaultValue
	}

	if configValue != nil {
		trimmed := strings.TrimSpace(*configValue)
		if trimmed != "" {
			return trimmed
		}
	}

	return defaultValue
}

func ResolveBool(envNames []string, flagSet bool, flagValue bool, configValue *bool, defaultValue bool) (bool, error) {
	if envName, value, ok := lookupNonEmptyEnv(envNames...); ok {
		parsedValue, err := strconv.ParseBool(value)
		if err != nil {
			return false, fmt.Errorf("invalid %s value %q: %w", envName, value, err)
		}
		return parsedValue, nil
	}

	if flagSet {
		return flagValue, nil
	}

	if configValue != nil {
		return *configValue, nil
	}

	return defaultValue, nil
}

func ResolveSeconds(envNames []string, flagSet bool, flagValue int, configValue *DurationSeconds, defaultValue int) (int, error) {
	if envName, value, ok := lookupNonEmptyEnv(envNames...); ok {
		seconds, err := ParseDurationSeconds(value)
		if err != nil {
			return 0, fmt.Errorf("invalid %s value %q: %w", envName, value, err)
		}
		return seconds, nil
	}

	if flagSet {
		return flagValue, nil
	}

	if configValue != nil {
		return configValue.Int(), nil
	}

	return defaultValue, nil
}

func ResolveInt(envNames []string, flagSet bool, flagValue int, configValue *int, defaultValue int) (int, error) {
	if envName, value, ok := lookupNonEmptyEnv(envNames...); ok {
		parsedValue, err := strconv.Atoi(value)
		if err != nil {
			return 0, fmt.Errorf("invalid %s value %q: %w", envName, value, err)
		}
		return parsedValue, nil
	}

	if flagSet {
		return flagValue, nil
	}

	if configValue != nil {
		return *configValue, nil
	}

	return defaultValue, nil
}

func HasNonEmptyEnv(envNames ...string) bool {
	_, _, ok := lookupNonEmptyEnv(envNames...)
	return ok
}

func loadJSON(path string, target any) error {
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		return nil
	}

	data, err := os.ReadFile(trimmedPath)
	if err != nil {
		return fmt.Errorf("read config %q: %w", trimmedPath, err)
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(target); err != nil {
		return fmt.Errorf("decode config %q: %w", trimmedPath, err)
	}

	if err = decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("decode config %q: expected single JSON object", trimmedPath)
	}

	return nil
}

func lookupNonEmptyEnv(envNames ...string) (string, string, bool) {
	for _, envName := range envNames {
		value, ok := os.LookupEnv(envName)
		if !ok {
			continue
		}

		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}

		return envName, trimmed, true
	}

	return "", "", false
}
