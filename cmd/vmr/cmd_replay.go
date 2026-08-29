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
	req := fs.String("req", "", "replay the record at this coordinate (\"basename:line\", as published in vmr-requests.json's \"req\" field or a Manifest's Req) — copy-paste ready: the audit file argument is optional with -req (omit it to search the current directory and config.yaml's log_dir, or pass a directory to search instead; an exact file path still requires its basename to match); mutually exclusive with -line and -ts")
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
	// The positional audit-file argument is normally required, EXCEPT with
	// -req: that locator carries its own file identity (a "basename:line"
	// coordinate) and can search for the file itself
	// — omit the argument entirely, or pass a directory to search instead
	// of cwd/log_dir. replay.selectRecord enforces the actual requirement;
	// this only bounds NArg so a typo'd extra argument still errors here
	// rather than being silently ignored.
	if fs.NArg() > 1 {
		return fmt.Errorf("usage: vmr replay [-c config.yaml] {-provider NAME | -print} [-line N | -ts TS | -req COORD] [flags] [audit.jsonl|.jsonl.zst|dir]")
	}
	var auditPathArg string
	if fs.NArg() == 1 {
		auditPathArg = fs.Arg(0)
	}
	opts := replay.Options{
		ConfigPath: *cfgPath,
		AuditPath:  auditPathArg,
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
