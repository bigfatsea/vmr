// Ver 2026-08-29, by Sonnet 5

// Journey severity: a one-word verdict (critical / warning / clean) for the
// incident report's headline, plus which finding drove it. A pure static
// lookup over Finding.Code — no new judgement, no new detection. Critical is
// reserved for the failure modes that mean the agent was actively burning
// resources or had lost the task (loops, goal drift, dropped constraints);
// everything else the detectors flag is a warning worth a look; no findings
// at all is clean.
package story

// Severity levels, ordered. String values are stable (rendered as-is into
// the dashboard's verdict stamp and usable as a machine anchor).
const (
	SeverityClean    = "clean"
	SeverityWarning  = "warning"
	SeverityCritical = "critical"
)

// criticalFindings are the codes that escalate a Journey to critical — the
// "actively going wrong", not merely "worth checking", failure modes.
var criticalFindings = map[FindingCode]bool{
	FindingExactRepeatToolCall:    true, // tool-call loop
	FindingUnadaptedRetry:         true, // retry loop, same broken args
	FindingNarrationWithoutAction: true, // talks in circles, never acts
	FindingSemanticOscillation:    true, // flip-flops between positions
	FindingGoalDrift:              true, // working on something else now
	FindingConstraintTextDropped:  true, // a compaction dropped its constraints
}

// findingLevel is one finding's severity — critical if in criticalFindings,
// else warning (every code the detectors emit is at least worth a look).
func findingLevel(c FindingCode) string {
	if criticalFindings[c] {
		return SeverityCritical
	}
	return SeverityWarning
}

// lowConfidenceFindings are detector codes too noisy to headline a Journey's
// verdict on their own. unverified_entity_reference fires on ~34% of the
// real corpus and its own detector doc comment hedges it as "a suspicious
// signal anchored on a tool falsification, not a confirmed hallucination"
// (it flags Go stdlib types and the project's own live endpoints as
// "证伪"). It still counts toward the severity LEVEL; JourneySeverity just
// skips it when choosing the driver unless it is the only finding at that
// level, so the dashboard's "probable cause" headline points at a more
// specific finding whenever one exists. See ANALYZE_SAMPLE_REPORTS_REVIEW
// 问题 2 / 问题 11.
var lowConfidenceFindings = map[FindingCode]bool{
	FindingUnverifiedEntityReference: true,
}

// JourneySeverity returns the Journey's worst severity and the finding that
// set it (earliest StepSeq at that level, then Code order — independent of
// the findings slice's own order). driver is "" only when level is clean.
// A lowConfidenceFindings code drives the verdict only when every finding
// at the worst level is low-confidence.
func JourneySeverity(findings []Finding) (level string, driver FindingCode) {
	level = SeverityClean
	for _, f := range findings {
		if findingLevel(f.Code) == SeverityCritical {
			level = SeverityCritical
			break
		}
		level = SeverityWarning
	}
	if level == SeverityClean {
		return SeverityClean, ""
	}
	if d, ok := pickDriver(findings, level, true); ok {
		return level, d
	}
	d, _ := pickDriver(findings, level, false)
	return level, d
}

// pickDriver returns the earliest-StepSeq finding at level (ties broken by
// Code order). skipLowConf drops lowConfidenceFindings from consideration;
// ok is false when that leaves nothing to pick.
func pickDriver(findings []Finding, level string, skipLowConf bool) (driver FindingCode, ok bool) {
	best := -1
	for _, f := range findings {
		if findingLevel(f.Code) != level {
			continue
		}
		if skipLowConf && lowConfidenceFindings[f.Code] {
			continue
		}
		if driver == "" || f.StepSeq < best || (f.StepSeq == best && f.Code < driver) {
			driver, best = f.Code, f.StepSeq
		}
	}
	return driver, driver != ""
}
