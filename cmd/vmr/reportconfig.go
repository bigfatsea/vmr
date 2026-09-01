// Ver 2026-08-01, by Sonnet 5

// vmr report/vmr story's own tiny sidecar config — report.yaml. Entirely
// separate from internal/config.Config: config.yaml is the router's
// deployment config (holds provider secrets, has a complex schema, and vmr
// report/vmr story routinely run without one at all, e.g. analyzing logs
// copied off-box). report.yaml can hold a real secret (llm_key), same as
// config.yaml, so it's .gitignore'd the same way — report.example.yaml is
// the committed template both files follow. See
// docs/VirtualModelRouter_Design_v4_Analytics.md's "配置与命令行" subsection
// for the full rationale. New report/story-only settings land here in the
// future, not in config.yaml.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"vmr/internal/i18n"
)

// reportConfig is report.yaml's schema — see report.example.yaml for the
// full annotated reference. Every field here is a fallback default only: an
// explicit command-line flag always wins over it, and the field's own
// absence (zero value) falls through to that flag's built-in default. The
// two bool fields are pointers so "absent from report.yaml" (fall through)
// and "explicitly set to false" (use false) are distinguishable — a plain
// bool can't represent that.
type reportConfig struct {
	Language       string `yaml:"language"`        // en (default) | zh
	Output         string `yaml:"output"`          // -o's default when -o isn't passed
	Details        *bool  `yaml:"details"`         // vmr report's -details default
	IncludePartial *bool  `yaml:"include_partial"` // vmr story's -include-partial default
	LLMAddr        string `yaml:"llm_addr"`        // vmr story's -llm-addr default
	LLMModel       string `yaml:"llm_model"`       // vmr story's -llm-model default
	LLMKey         string `yaml:"llm_key"`         // vmr story's -llm-key default; plaintext or "${SOME_ENV_VAR}"
	LLMCacheDir    string `yaml:"llm_cache_dir"`   // vmr story's -llm-cache-dir default; "" everywhere = no caching, never an implicit path
	// SelfTrafficClientTags (P6.4) extends the self-traffic exclusion set
	// beyond the one tag auto-derived from LLMKey — needed only when
	// -llm-addr traffic was generated under a DIFFERENT, e.g. rotated,
	// credential than the one currently configured. Most deployments
	// leave this empty: the LLMKey-derived tag alone is sufficient. See
	// selftraffic.go.
	SelfTrafficClientTags []string `yaml:"self_traffic_client_tags"`
	// Currency is vmr report's -currency default — the currency $ cost
	// estimates are DISPLAYED in, independent of whatever currency they were
	// actually computed in (config.yaml's pricing.currency, or USD with no
	// config.yaml reachable). Empty = show whatever currency computation
	// used, no conversion (today's behavior, unchanged).
	Currency string `yaml:"currency"`
	// ExchangeRate is a "1 USD = X <code>" map (same shape/semantics as
	// config.yaml's pricing.exchange_rate — see internal/pricing.
	// FactorBetween), consulted ONLY for the Currency conversion above. Lets
	// -currency work even with no config.yaml reachable at all (report.yaml
	// is meant to stand alone); when config.yaml IS reachable, entries here
	// win over its pricing.exchange_rate on a matching key.
	ExchangeRate map[string]float64 `yaml:"exchange_rate"`
}

// defaultReportConfigFile is the cwd-relative path auto-loaded when
// -report-config isn't given — auto-loaded if present, else silently
// skipped (falls back to defaults).
const defaultReportConfigFile = "report.yaml"

// reportEnvRe/expandReportEnv: the same "${NAME} -> os.Getenv(NAME), unset
// -> empty, bare $ stays literal" contract internal/config's own expandEnv
// applies to config.yaml — duplicated here rather than imported, since this
// file's whole point is staying independent of internal/config (see the
// package comment above). The injection guards are duplicated with it: a
// value containing a newline, ": " or " #" is spliced in before YAML
// parsing, so it could change the document's structure or silently truncate
// the value at a YAML comment — report.yaml carries llm_key, so a " #"
// suffix would cut a secret short with no error at all, surfacing only as a
// mysterious 401. Same fail-fast rule config.yaml applies; same known
// residual gap too (a value inside a flow collection could still inject via
// a comma).
var reportEnvRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func expandReportEnv(s string) (string, error) {
	var badVar string
	out := reportEnvRe.ReplaceAllStringFunc(s, func(m string) string {
		name := m[2 : len(m)-1]
		v := os.Getenv(name)
		if badVar == "" && (strings.Contains(v, "\n") || strings.Contains(v, ": ") || strings.Contains(v, " #")) {
			badVar = name
		}
		return v
	})
	if badVar != "" {
		return "", fmt.Errorf("environment variable %q's value contains a newline, \": \", or \" #\" — expanding it into report.yaml could change the document's structure or silently truncate the value at a YAML comment; remove those characters from the value (or avoid interpolating it) before retrying", badVar)
	}
	return out, nil
}

func loadReportConfig(path string) (reportConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return reportConfig{}, err
	}
	expanded, err := expandReportEnv(string(data))
	if err != nil {
		return reportConfig{}, err
	}
	var rc reportConfig
	dec := yaml.NewDecoder(bytes.NewReader([]byte(expanded)))
	dec.KnownFields(true) // same strict-YAML stance config.yaml itself takes: a typo'd key is a load error, not a silent no-op
	if err := dec.Decode(&rc); err != nil && err != io.EOF {
		return reportConfig{}, err
	}
	// io.EOF means the file had no YAML document at all (0 bytes, or only
	// comments/whitespace) — a legitimate empty config, not a parse
	// failure; every field then falls through to its own default.
	return rc, nil
}

// resolveReportConfig loads report.yaml (or -report-config's path) exactly
// once per command invocation — every resolve*/flagPassed call below shares
// this one load rather than re-reading the file per field. A missing file
// at the default path is silently a no-op (most runs have no report.yaml
// at all, e.g. analyzing a log archive copied off-box — that is "not
// configured", a legitimate state). Everything else is a hard error that
// exits: a file that exists but fails to parse would otherwise silently
// disable ALL of report.yaml's settings — self-traffic exclusion, every
// llm_* field, currency — with at most a warning mixed into normal output;
// KnownFields strictness only pays for itself if the failure is loud. An
// explicit -report-config path that doesn't exist is the user's own pointer
// and errors too.
func resolveReportConfig(reportConfigPath string, tw io.Writer) reportConfig {
	rc, err := resolveReportConfigErr(reportConfigPath)
	if err == nil {
		return rc
	}
	reportConfigFatal(tw, err)
	return reportConfig{} // unreachable
}

// reportConfigFatal terminates the process on an unusable report config. A
// package-level func so tests can intercept the exit — there is no in-process
// way to survive os.Exit.
var reportConfigFatal = func(tw io.Writer, err error) {
	fmt.Fprintf(tw, "error: %v\n", err)
	os.Exit(1)
}

// resolveReportConfigErr is resolveReportConfig's testable core: it returns
// the decision instead of exiting, so the hard-error policy can be asserted
// without killing the test process.
func resolveReportConfigErr(reportConfigPath string) (reportConfig, error) {
	explicit := reportConfigPath != ""
	if reportConfigPath == "" {
		reportConfigPath = defaultReportConfigFile
	}
	rc, err := loadReportConfig(reportConfigPath)
	if err == nil {
		return rc, nil
	}
	if !explicit && os.IsNotExist(err) {
		return reportConfig{}, nil
	}
	return reportConfig{}, fmt.Errorf("report config %s: %w", reportConfigPath, err)
}

// resolveLanguage: -lang > report.yaml's language > i18n.EN. An explicit
// -lang that fails to parse is a hard error (the user typed it themselves);
// an invalid report.yaml language degrades to English with a warning
// instead, the same best-effort spirit as the rest of report.yaml.
func resolveLanguage(langFlag string, rc reportConfig, tw io.Writer) (i18n.Lang, error) {
	if langFlag != "" {
		return i18n.Parse(langFlag)
	}
	if rc.Language == "" {
		return i18n.EN, nil
	}
	lang, err := i18n.Parse(rc.Language)
	if err != nil {
		fmt.Fprintf(tw, "warning: report.yaml language=%q is invalid, defaulting to English\n", rc.Language)
		return i18n.EN, nil
	}
	return lang, nil
}

// resolveString applies the shared "-flag > report.yaml > built-in default"
// merge order to a plain string field: flagVal wins if the user passed it
// (its zero value, "", stands in for "not passed" — every string flag this
// applies to defaults to "" for exactly this reason), else rcVal if
// report.yaml set it, else def. def is "" for every llm_* field: an unset
// -llm-addr/-llm-cache-dir etc. must leave that layer off, never point at an
// implicit path or address.
func resolveString(flagVal, rcVal, def string) string {
	if flagVal != "" {
		return flagVal
	}
	if rcVal != "" {
		return rcVal
	}
	return def
}

// resolveStringExplicit is resolveString's flagPassed-aware counterpart,
// same shape as resolveBool: for a flag where the caller needs to
// explicitly pass "" to clear rcVal (e.g. -llm-addr ” to force off LLM
// calls despite report.yaml configuring a default address), resolveString's
// "empty means not passed" rule can't express that — explicit says so
// instead.
func resolveStringExplicit(explicit bool, flagVal, rcVal, def string) string {
	if explicit {
		return flagVal
	}
	if rcVal != "" {
		return rcVal
	}
	return def
}

// resolveBool applies the same merge order to a bool flag, where "not
// passed" can't be read off the flag's own value (false is a valid explicit
// choice) — flagPassed says whether the user actually typed the flag.
// flagVal, when !explicit, already equals that flag's own built-in default,
// so there is no separate def parameter here.
func resolveBool(explicit, flagVal bool, rcVal *bool) bool {
	if !explicit && rcVal != nil {
		return *rcVal
	}
	return flagVal
}

// flagPassed reports whether name was explicitly given on the command line
// (as opposed to sitting at its flag.*'s own default) — the standard
// flag.FlagSet.Visit idiom, needed because a bool flag's zero value (false)
// is itself a valid explicit choice and can't be distinguished from "not
// passed" any other way.
func flagPassed(fs *flag.FlagSet, name string) bool {
	passed := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			passed = true
		}
	})
	return passed
}
