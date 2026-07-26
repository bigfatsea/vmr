// Ver 2026-07-26, by Sonnet 5

// vmr — Virtual Model Router. Single binary, config driven.
//
//	vmr start    -c config.yaml   run the router
//	vmr check    -c config.yaml   validate config and print a summary
//	vmr status   -c config.yaml   show endpoint health of a running instance
//	vmr report   <audit.jsonl>    aggregate audit logs into usage statistics (default -o: ./reports)
//	vmr dirs     {log|cache}      print the config's effective log_dir / image_cache_dir (vmr.sh uses this)
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
	case "dirs":
		err = cmdDirs(os.Args[2:])
	case "replay":
		err = cmdReplay(os.Args[2:])
	case "diagnose":
		err = cmdDiagnose(os.Args[2:])
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
	fmt.Fprintln(os.Stderr, `usage: vmr <start|check|status> [-c config.yaml]
       vmr report [-o dir] [-details=false] [-pricing pricing.yaml] <audit.jsonl|glob>...   (default -o: ./reports; auto-loads ./pricing.yaml if -pricing omitted)
       vmr dirs [-c config.yaml] {log|cache}
       vmr diagnose [-c config.yaml] [-no-test-routing] [-json]
       vmr replay [-c config.yaml] -provider NAME [-line N | -ts TS] [flags] <audit.jsonl|.jsonl.zst>
       vmr replay [-c config.yaml] -provider NAME -detail FILE [flags]`)
}
