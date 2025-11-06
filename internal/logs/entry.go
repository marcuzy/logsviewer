package logs

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// LogEntry represents a decoded JSON log line coming from a watched file.
type LogEntry struct {
	Path          string
	Timestamp     time.Time
	TimestampText string
	Message       string
	Extras        map[string]string
	Fields        map[string]any
	Raw           string
}

// PrettyJSON returns a prettified version of the original raw JSON payload.
func (e LogEntry) PrettyJSON() string {
	if e.Raw == "" {
		return ""
	}
	var buf map[string]any
	if err := json.Unmarshal([]byte(e.Raw), &buf); err != nil {
		return e.Raw
	}
	data, err := json.MarshalIndent(buf, "", "  ")
	if err != nil {
		return e.Raw
	}
	return string(data)
}

// DisplayTimestamp returns the best-effort human timestamp associated with the entry.
func (e LogEntry) DisplayTimestamp() string {
	if !e.Timestamp.IsZero() {
		return e.Timestamp.Local().Format("2006-01-02 15:04:05")
	}
	return e.TimestampText
}

// ExtraValue returns the string value for a configured extra field.
func (e LogEntry) ExtraValue(name string) string {
	if name == "" {
		return ""
	}
	if e.Extras == nil {
		return ""
	}
	return e.Extras[name]
}

func parseEntry(path string, line string, cfg ParserConfig) (LogEntry, error) {
	fields := make(map[string]any)
	if err := json.Unmarshal([]byte(line), &fields); err != nil {
		return LogEntry{}, fmt.Errorf("parse %s: %w", path, err)
	}

	entry := LogEntry{
		Path:   path,
		Fields: fields,
		Raw:    line,
		Extras: make(map[string]string),
	}

	entry.Timestamp, entry.TimestampText = extractTimestamp(fields[cfg.TimestampField])
	entry.Message = extractString(fields[cfg.MessageField])

	for _, name := range cfg.ExtraFields {
		switch name {
		case "@file":
			entry.Extras[name] = path
		default:
			entry.Extras[name] = extractString(fields[name])
		}
	}

	return entry, nil
}

// ParserConfig controls how JSON entries are interpreted.
type ParserConfig struct {
	TimestampField string
	MessageField   string
	ExtraFields    []string
}

func extractTimestamp(value any) (time.Time, string) {
	switch v := value.(type) {
	case string:
		if ts, ok := parseTimeString(v); ok {
			return ts, ts.Format(time.RFC3339Nano)
		}
		return time.Time{}, v
	case float64:
		ts := parseUnixNumber(v)
		return ts, ts.Format(time.RFC3339Nano)
	case json.Number:
		if f, err := v.Float64(); err == nil {
			ts := parseUnixNumber(f)
			return ts, ts.Format(time.RFC3339Nano)
		}
	case int64:
		ts := parseUnixNumber(float64(v))
		return ts, ts.Format(time.RFC3339Nano)
	case int:
		ts := parseUnixNumber(float64(v))
		return ts, ts.Format(time.RFC3339Nano)
	case nil:
		return time.Time{}, ""
	}
	return time.Time{}, fmt.Sprint(value)
}

func parseTimeString(input string) (time.Time, bool) {
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.999999-07:00",
		"2006-01-02 15:04:05.999999",
		"2006-01-02 15:04:05",
		"02/01/2006 15:04:05",
	}
	for _, layout := range layouts {
		if ts, err := time.Parse(layout, input); err == nil {
			return ts, true
		}
	}
	if epoch, err := strconv.ParseFloat(input, 64); err == nil {
		return parseUnixNumber(epoch), true
	}
	return time.Time{}, false
}

func parseUnixNumber(n float64) time.Time {
	switch {
	case n > 1e18:
		return time.Unix(0, int64(n))
	case n > 1e15:
		return time.Unix(0, int64(n))
	case n > 1e12:
		return time.Unix(0, int64(n*1e6))
	case n > 1e9:
		return time.Unix(int64(n), 0)
	default:
		return time.Unix(0, int64(n*1e9))
	}
}

func extractString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	case nil:
		return ""
	case json.Number:
		return v.String()
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int64:
		return strconv.FormatInt(v, 10)
	case int:
		return strconv.Itoa(v)
	case bool:
		return strconv.FormatBool(v)
	case map[string]any, []any:
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(data)
	default:
		return fmt.Sprintf("%v", v)
	}
}
