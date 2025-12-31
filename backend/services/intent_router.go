package services

import (
	"regexp"
	"strings"
)

// IntentRouter inspects a user's NL query and returns a normalized intent and
// an ordered list of agents to run. It uses keyword matching and simple
// heuristics. The returned intent should be one of:
//   - simulation
//   - incident_followup
//   - deep_followup
//   - status_query
//   - postmortem
//   - unknown
func IntentRouter(query string, incidentId string) (string, []string) {
	q := strings.ToLower(strings.TrimSpace(query))
	var agents []string
	intent := "unknown"

	// quick regex utility for word boundaries
	hasWord := func(pattern string) bool {
		r := regexp.MustCompile(pattern)
		return r.MatchString(q)
	}

	// Simulation / what-if style questions
	if hasWord(`\bwhat if\b`) || hasWord(`\bsimulate\b`) || hasWord(`\bwould happen if\b`) {
		intent = "simulation"
		agents = append(agents, "metrics_agent", "rca_historical_agent")
	} else if hasWord(`\bwhy\b`) || hasWord(`root cause`) || hasWord(`cause of`) || hasWord(`reason for`) || hasWord(`explain`) {
		// Explicit why / root cause questions
		intent = "incident_followup"
		agents = append(agents, "logs_traces_agent", "metrics_agent", "rca_historical_agent")
	} else if hasWord(`\bverify\b`) || hasWord(`\brecheck\b`) || hasWord(`\bprove\b`) || hasWord(`\bvalidate\b`) {
		// Verify / validate / prove - deeper checks across systems
		intent = "deep_followup"
		agents = append(agents, "metrics_agent", "logs_traces_agent", "rca_historical_agent")
	} else if hasWord(`\bnow\b`) || hasWord(`\bstatus\b`) || hasWord(`\bstill\b`) || hasWord(`\bis it up\b`) {
		// Status queries asking about current state
		intent = "status_query"
		agents = append(agents, "metrics_agent")
	} else if hasWord(`\bpostmortem\b`) || hasWord(`\bafter\b`) || hasWord(`\bretrospective\b`) {
		// Postmortem / after-the-fact analysis
		intent = "postmortem"
		agents = append(agents, "rca_historical_agent", "metrics_agent")
	} else if incidentId != "" {
		// If we have an incident id, assume followup intent by default
		intent = "incident_followup"
		agents = append(agents, "metrics_agent", "logs_traces_agent", "rca_historical_agent")
	}

	// Note: moderator activation is handled by the moderator process itself;
	// do not force `moderator_agent` into per-message `agents` lists here.
	// The moderator will always start for any `incidents_created` event and
	// will exclude itself from its own wait list.

	return intent, agents
}
