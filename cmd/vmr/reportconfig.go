// Ver 2026-08-01, by Sonnet 5

// vmr report/vmr story's own tiny sidecar config — report.yaml. Entirely
// separate from internal/config.Config: config.yaml is the router's
// deployment config (holds provider secrets, has a complex schema, and vmr
// report/vmr story routinely run without one at all, e.g. analyzing logs
// copied off-box). report.yaml has nothing sensitive in it and is safe to
// commit alongside a log archive. See
// docs/VirtualModelRouter_Design_v4_Analytics.md's "配置与命令行" subsection
// for the full rationale. New report/story-only settings land here in the future,
// not in config.yaml.
package main

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"

	"vmr/internal/i18n"
)

// reportConfig is report.yaml's schema.
type reportConfig struct {
	Language string `yaml:"language"` // en (default) | zh
}

// defaultReportConfigFile is the cwd-relative path auto-loaded when
// -report-config isn't given — same "auto-load if present, else silently
// skip" convention cmd_report.go's own -pricing/pricing.yaml already uses.
const defaultReportConfigFile = "report.yaml"

func loadReportConfig(path string) (reportConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return reportConfig{}, err
	}
	var rc reportConfig
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true) // same strict-YAML stance config.yaml itself takes: a typo'd key is a load error, not a silent no-op
	if err := dec.Decode(&rc); err != nil {
		return reportConfig{}, err
	}
	return rc, nil
}

// resolveLanguage: -lang > report.yaml's language > i18n.EN. Entirely
// independent of resolveInputPaths/config.Load — report.yaml has nothing to
// do with config.yaml or log_dir resolution, so there is no shared loading
// path to reuse or avoid colliding with. reportConfigPath == "" means
// "auto-detect ./report.yaml"; an explicit -lang always wins and, if
// invalid, is a hard error (the user typed it, so it's not a best-effort
// case the way an absent/malformed report.yaml is).
func resolveLanguage(langFlag, reportConfigPath string, tw io.Writer) (i18n.Lang, error) {
	if langFlag != "" {
		return i18n.Parse(langFlag)
	}
	explicit := reportConfigPath != ""
	if reportConfigPath == "" {
		reportConfigPath = defaultReportConfigFile
	}
	rc, err := loadReportConfig(reportConfigPath)
	if err != nil {
		if explicit {
			// The user typed -report-config themselves — a missing/unreadable
			// file at that exact path is likely a typo, not a "no config"
			// no-op, so it's worth a warning even though we still degrade to
			// English rather than fail the whole report/story run over it.
			fmt.Fprintf(tw, "warning: -report-config %s not found or unreadable, defaulting to English: %v\n", reportConfigPath, err)
		} else if _, statErr := os.Stat(reportConfigPath); statErr == nil {
			fmt.Fprintf(tw, "warning: found %s but failed to load it, defaulting to English: %v\n", reportConfigPath, err)
		}
		return i18n.EN, nil
	}
	if rc.Language == "" {
		return i18n.EN, nil
	}
	lang, err := i18n.Parse(rc.Language)
	if err != nil {
		fmt.Fprintf(tw, "warning: %s language=%q is invalid, defaulting to English\n", reportConfigPath, rc.Language)
		return i18n.EN, nil
	}
	return lang, nil
}
