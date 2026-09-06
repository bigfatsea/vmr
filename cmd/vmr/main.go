// Ver 2026-07-29 11:35, by Sonnet 5

// vmr — Virtual Model Router. Single binary, config driven.
//
//	vmr start    -c config.yaml   run the router
//	vmr check    -c config.yaml   validate config and print a summary (or, with a trailing log|cache arg, just that resolved directory — vmr.sh uses this)
//	vmr status   -c config.yaml   show identity + endpoint health of a running instance
//	             -addr host:port  ... of whatever instance holds that port instead (no config needed)
//	vmr analyze  [audit.jsonl]    the single analysis entry point: default is the full navigable suite (aggregate report + journey half) in one call; -journey/-compare/-benchmark zoom into exactly one journey-side view instead

//	vmr diagnose -c config.yaml   validate config, test DNS/TLS/connectivity to every provider, preview routing
//	vmr smoke   -c config.yaml   fire a minimal real request at every configured backend through a running vmr (warms quota buckets, proves E2E reachability; -provider/-target-model/-model filter the run, pinned via X-VMR-* headers)
//	vmr replay   -provider NAME <audit.jsonl>   rebuild and resend one request from an audit record (-line/-ts/-req to pick which; -print to just read it, no -provider needed)
//
// Each subcommand lives in its own cmd_*.go file; this file is only the
// dispatcher, usage text, and the adapter blank-import registration point.
package main

import (
	"fmt"
	"os"

	// Adding a provider type = one blank import here.
	_ "vmr/internal/adapter/anthropic"
	_ "vmr/internal/adapter/openai"
	_ "vmr/internal/adapter/openairesponses"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	// An explicit help request exits 0, matching every subcommand's own
	// `-h` (Go's flag.ExitOnError does that for them). Bare `vmr` with no
	// command stays exit 2 above — that is a usage error, not a help ask.
	case "-h", "-help", "--help", "help":
		usage()
		return
	case "start":
		err = cmdStart(os.Args[2:])
	case "check":
		err = cmdCheck(os.Args[2:])
	case "status":
		err = cmdStatus(os.Args[2:])
	case "analyze":
		err = cmdAnalyze(os.Args[2:])
	case "replay":
		err = cmdReplay(os.Args[2:])
	case "smoke":
		err = cmdSmoke(os.Args[2:])
	case "diagnose":
		err = cmdDiagnose(os.Args[2:])
	case "version":
		err = cmdVersion(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "vmr:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: vmr <start|check> [-c config.yaml]
       vmr check [-c config.yaml] [log|cache]   (prints just that resolved directory instead of the full summary)
       vmr status [-c config.yaml | -addr host:port] [-key KEY] [-brief]   (./vmr.sh ps lists every instance on this machine)
       vmr analyze [-c config.yaml] [-o dir] [-journey id | -compare id1,id2 | -benchmark] [-render-all] [-details] [-include-partial] [-include-self-traffic] [-lang en|zh] [-currency CODE] [audit.jsonl|glob]...   (single analysis entry point; no selector = default suite: journey half then report half, category=task candidates only unless -render-all; -journey/-compare/-benchmark zoom into exactly one journey-side view, no report half)
       vmr diagnose [-c config.yaml] [-no-test-routing] [-json]
       vmr smoke [-c config.yaml] [-addr host:port] [-key KEY] [-timeout D] [-parallel N] [-provider NAME] [-target-model NAME] [-model NAME] [-json]
       vmr replay [-c config.yaml] {-provider NAME | -print} [-line N | -ts TS | -req COORD] [flags] [audit.jsonl|.jsonl.zst|dir]   (the file argument is required for -line/-ts; optional for -req, which can search cwd/log_dir for its coordinate's basename)
       vmr version`)
}
