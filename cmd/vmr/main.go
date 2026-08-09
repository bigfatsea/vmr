// Ver 2026-07-29 11:35, by Sonnet 5

// vmr — Virtual Model Router. Single binary, config driven.
//
//	vmr start    -c config.yaml   run the router
//	vmr check    -c config.yaml   validate config and print a summary (or, with a trailing log|cache arg, just that resolved directory — vmr.sh uses this)
//	vmr status   -c config.yaml   show identity + endpoint health of a running instance
//	             -addr host:port  ... of whatever instance holds that port instead (no config needed)
//	vmr report   [audit.jsonl]    aggregate audit logs into usage statistics (default -o: ./reports; default input: -c config.yaml's log_dir/vmr-audit-*)
//	vmr story    [audit.jsonl]    render one agent task's full execution history as a narrative (default -o: ./reports; default input: -c config.yaml's log_dir/vmr-audit-*)
//	vmr diagnose -c config.yaml   validate config, test DNS/TLS/connectivity to every provider, preview routing
//	vmr replay   -provider NAME <audit.jsonl>   rebuild and resend one request from an audit record (or -detail FILE, no audit file needed)
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
	case "start":
		err = cmdStart(os.Args[2:])
	case "check":
		err = cmdCheck(os.Args[2:])
	case "status":
		err = cmdStatus(os.Args[2:])
	case "report":
		err = cmdReport(os.Args[2:])
	case "story":
		err = cmdStory(os.Args[2:])
	case "replay":
		err = cmdReplay(os.Args[2:])
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
       vmr status [-c config.yaml | -addr host:port] [-brief]   (./vmr.sh ps lists every instance on this machine)
       vmr report [-c config.yaml] [-o dir] [-details=false] [-lang en|zh] [audit.jsonl|glob]...   (default -o: ./reports; $ estimates use the built-in standard price table plus -c's config.yaml pricing overrides, if reachable; -lang default: report.yaml's language, or en; no input files => -c's log_dir/vmr-audit-*)
       vmr story [-c config.yaml] [-journey id | -render-all | -compare id1,id2] [-include-partial] [-show-ungrouped] [-o dir] [-lang en|zh] [audit.jsonl|glob]...   (default -o: ./reports; no -journey/-render-all lists candidates; -lang default: report.yaml's language, or en; no input files => -c's log_dir/vmr-audit-*)
       vmr diagnose [-c config.yaml] [-no-test-routing] [-json]
       vmr replay [-c config.yaml] -provider NAME [-line N | -ts TS] [flags] <audit.jsonl|.jsonl.zst>
       vmr replay [-c config.yaml] -provider NAME -detail FILE [flags]
       vmr version`)
}
