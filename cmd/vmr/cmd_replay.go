// Ver 2026-07-26, by Sonnet 5
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"

	"vmr/internal/replay"
)

// cmdReplay rebuilds and resends one request from an audit record — see
// internal/replay for the mechanics (same adapter.BuildRequest path vmr
// itself uses, so the replayed request matches what vmr originally sent).
func cmdReplay(args []string) error {
	fs := flag.NewFlagSet("replay", flag.ExitOnError)
	cfgPath := fs.String("c", "config.yaml", "path to config file")
	line := fs.Int("line", 0, "1-based line number to replay (default: the last parsable record in the file); mutually exclusive with -ts and -req")
	ts := fs.String("ts", "", "replay the record whose timestamp matches this (millisecond precision; accepts either vmr-requests.json's \"ts\" or the raw audit.jsonl \"ts\" field verbatim); mutually exclusive with -line and -req")
	req := fs.String("req", "", "replay the record at this coordinate (\"basename:line\", as published in vmr-requests.json's \"req\" field or a Manifest's Req) — the audit file argument's basename must match; mutually exclusive with -line and -ts")
	print := fs.Bool("print", false, "print the resolved record's raw JSON to stdout and exit — no -provider needed, nothing built or sent; combine with -line/-ts/-req to pick which record")
	provider := fs.String("provider", "", "provider to replay against (required unless -print; providers.<protocol>.<name>)")
	model := fs.String("model", "", "override the upstream model name (default: resolved from config for -provider under the record's virtual model)")
	protocol := fs.String("protocol", "", "override the protocol (default: the record's own protocol)")
	streamFlag := fs.String("stream", "", "force stream on/off: true|false (default: the record's own value)")
	dryRun := fs.Bool("dry-run", false, "print the request that would be sent, without sending it")
	recordPath := fs.String("record", "", "append the replay's request/response to this audit JSONL file")
	maxTime := fs.Duration("max-time", 0, "upstream timeout for this replay (default: config timeouts.response_header/stream_idle)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: vmr replay [-c config.yaml] {-provider NAME | -print} [-line N | -ts TS | -req COORD] [flags] <audit.jsonl|.jsonl.zst>")
	}
	opts := replay.Options{
		ConfigPath: *cfgPath,
		AuditPath:  fs.Arg(0),
		Line:       *line,
		TS:         *ts,
		Req:        *req,
		Print:      *print,
		Provider:   *provider,
		Model:      *model,
		Protocol:   *protocol,
		DryRun:     *dryRun,
		RecordPath: *recordPath,
		MaxTime:    *maxTime,
	}
	if *streamFlag != "" {
		b, err := strconv.ParseBool(*streamFlag)
		if err != nil {
			return fmt.Errorf("-stream: %w", err)
		}
		opts.Stream = &b
	}
	return replay.Run(context.Background(), opts, os.Stdout)
}
