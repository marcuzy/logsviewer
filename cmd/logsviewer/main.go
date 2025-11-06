package main

import (
	"context"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/pflag"

	"github.com/marcuzy/logsviewer/internal/config"
	"github.com/marcuzy/logsviewer/internal/logs"
	"github.com/marcuzy/logsviewer/internal/ui"
)

func main() {
	flags := pflag.NewFlagSet("logsviewer", pflag.ExitOnError)
	configPath := flags.StringP("config", "c", "", "path to configuration file")
	files := flags.StringSliceP("file", "f", nil, "log file(s) to follow")
	timestampField := flags.String("timestamp-field", "", "JSON field containing the timestamp")
	messageField := flags.String("message-field", "", "JSON field containing the message")
	extraFields := flags.StringSlice("extra-field", nil, "additional field(s) to show in the log list (repeatable)")
	tailLines := flags.Int("tail", -1, "number of lines to read from the end on startup")
	maxEntries := flags.Int("max-entries", 0, "maximum number of log entries to keep in memory")
	showHelp := flags.BoolP("help", "h", false, "show usage")

	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage: %s [flags]\n\nFlags:\n", os.Args[0])
		flags.PrintDefaults()
	}

	if err := flags.Parse(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error parsing flags: %v\n", err)
		os.Exit(1)
	}

	if *showHelp {
		flags.Usage()
		return
	}

	var tailPtr *int
	if flags.Changed("tail") {
		tailPtr = tailLines
	}

	var maxPtr *int
	if flags.Changed("max-entries") {
		maxPtr = maxEntries
	}

	overrideExtras := []string(nil)
	if flags.Changed("extra-field") {
		overrideExtras = *extraFields
	}

	cfg, err := config.Load(config.Flags{
		ConfigPath:     *configPath,
		Files:          *files,
		TailLines:      tailPtr,
		MaxEntries:     maxPtr,
		TimestampField: *timestampField,
		MessageField:   *messageField,
		ExtraFields:    overrideExtras,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tailer := logs.NewTailer(cfg.Files, logs.Options{
		Parser: logs.ParserConfig{
			TimestampField: cfg.TimestampField,
			MessageField:   cfg.MessageField,
			ExtraFields:    cfg.ExtraFields,
		},
		TailLines: cfg.TailLines,
	})

	entriesCh, errsCh := tailer.Start(ctx)

	m := ui.NewModel(ui.Options{
		Entries:  entriesCh,
		Errors:   errsCh,
		Cancel:   cancel,
		Extra:    cfg.ExtraFields,
		MaxItems: cfg.MaxEntries,
	})

	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "program error: %v\n", err)
		os.Exit(1)
	}
}
