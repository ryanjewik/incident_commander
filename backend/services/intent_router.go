package services

import "strings"

func ContainsAny(mainString string, list []string) bool {
	for _, substr := range list {
		if strings.Contains(mainString, substr) {
			return true
		}
	}
	return false
}

func IntentRouter(query string, incidentId string) string {
	var intent string
	var agents []string

	if ContainsAny(query, []string{"what if", "simulate", "would happen if"}) {
		intent = "simulation"
		agents = append(agents, "SLO", "Cost", "Moderator") //RCA?
	} else if ContainsAny(query, []string{"why", "root cause", "cause of", "reason for", "explain"}) {
		intent = "incident_followup"
		agents = append(agents, "Moderator") //RCA or Logs or Metrics?
	} else if ContainsAny(query, []string{"verify", "recheck", "prove", "validate"}) {
		intent = "deep_followup"
		agents = append(agents, "Logs", "RCA", "Moderator") //metrics, deploy
	} else if ContainsAny(query, []string{"still", "now", "current status"}) {
		intent = "status_query"
		agents = append(agents, "SLO", "Moderator") //Metrics?
	} else if ContainsAny(query, []string{"after", "postmortem"}) {
		intent = "postmortem"
	} else if incidentId != "" /* check if incidentId is in database AND check if the status (like active or new) */ {
		intent = "incident_followup"
		agents = append(agents, "RCA", "SLO", "Moderator") //Cost, security?
	} else if incidentId == "" /*check if incidentId is in database but status is resovled or ignored*/ {
		intent = "status_query"
	}

	return intent
}
