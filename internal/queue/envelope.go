// Package queue defines the analysis job envelope shared with Codehound.
package queue

import (
	"encoding/json"
	"time"
)

const (
	// NameAnalysis is the pgmq queue Core publishes and Codehound consumes.
	NameAnalysis = "oktant_analysis"
	// TypeAnalysisRequested is the event type for a new scan.
	TypeAnalysisRequested = "analysis.requested"
	// EnvelopeVersion is the envelope schema version.
	EnvelopeVersion = 1
)

// Phase1Analyzers is the requested_analyzers list Codehound expects in Phase 1.
var Phase1Analyzers = []string{"sast", "secrets", "dependencies"}

// Access is the short-lived credential Codehound uses to clone.
// Never log this struct.
type Access struct {
	Kind      string `json:"kind"`
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

// AnalysisPayload is the payload of analysis.requested.
type AnalysisPayload struct {
	AnalysisRunID      string   `json:"analysis_run_id"`
	OrgID              string   `json:"org_id"`
	ProjectID          string   `json:"project_id"`
	RepositoryID       string   `json:"repository_id"`
	Provider           string   `json:"provider"`
	RepoFullName       string   `json:"repo_full_name"`
	CloneURL           string   `json:"clone_url"`
	DefaultBranch      string   `json:"default_branch"`
	CommitSHA          string   `json:"commit_sha"`
	Ref                string   `json:"ref"`
	Trigger            string   `json:"trigger"`
	AnalysisType       string   `json:"analysis_type"`
	RequestedAnalyzers []string `json:"requested_analyzers"`
	InstallationID     int64    `json:"installation_id"`
	Access             Access   `json:"access"`
}

// Envelope is the event wrapper from the platform event contract.
// correlation_id is required by Codehound and must be a UUID.
// actor_user_id is optional.
type Envelope struct {
	EventID       string          `json:"event_id"`
	Type          string          `json:"type"`
	Version       int             `json:"version"`
	OccurredAt    string          `json:"occurred_at"`
	OrgID         string          `json:"org_id"`
	ProjectID     string          `json:"project_id"`
	ActorUserID   string          `json:"actor_user_id,omitempty"`
	CorrelationID string          `json:"correlation_id"`
	Payload       AnalysisPayload `json:"payload"`
}

// RequestedAnalyzers returns the Phase 1 scanner set.
// Codehound runs createDefaultSecurityPipeline regardless of analysis_type.
func RequestedAnalyzers(_ string) []string {
	return append([]string(nil), Phase1Analyzers...)
}

// FormatTime is the ISO-8601 UTC timestamp Codehound parses.
func FormatTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

// NewAnalysisRequested builds a version-1 analysis.requested envelope.
func NewAnalysisRequested(eventID string, occurredAt time.Time, actorUserID, correlationID string, payload AnalysisPayload) Envelope {
	if len(payload.RequestedAnalyzers) == 0 {
		payload.RequestedAnalyzers = RequestedAnalyzers(payload.AnalysisType)
	}
	return Envelope{
		EventID:       eventID,
		Type:          TypeAnalysisRequested,
		Version:       EnvelopeVersion,
		OccurredAt:    FormatTime(occurredAt),
		OrgID:         payload.OrgID,
		ProjectID:     payload.ProjectID,
		ActorUserID:   actorUserID,
		CorrelationID: correlationID,
		Payload:       payload,
	}
}

// MarshalJSONBytes encodes env without pretty-printing.
func MarshalJSONBytes(env Envelope) ([]byte, error) {
	return json.Marshal(env)
}
