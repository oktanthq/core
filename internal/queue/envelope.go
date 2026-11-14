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
type Envelope struct {
	EventID       string          `json:"event_id"`
	Type          string          `json:"type"`
	Version       int             `json:"version"`
	OccurredAt    string          `json:"occurred_at"`
	OrgID         string          `json:"org_id"`
	ProjectID     string          `json:"project_id"`
	ActorUserID   string          `json:"actor_user_id,omitempty"`
	CorrelationID string          `json:"correlation_id,omitempty"`
	Payload       AnalysisPayload `json:"payload"`
}

// RequestedAnalyzers returns the Phase 1 scanner set for an analysis type.
func RequestedAnalyzers(analysisType string) []string {
	switch analysisType {
	case "security":
		return []string{"sast", "secrets", "dependencies"}
	case "quality":
		return []string{"quality"}
	case "full":
		return []string{"sast", "secrets", "dependencies", "quality"}
	default:
		return nil
	}
}

// NewAnalysisRequested builds a version-1 analysis.requested envelope.
func NewAnalysisRequested(eventID string, occurredAt time.Time, actorUserID, correlationID string, payload AnalysisPayload) Envelope {
	return Envelope{
		EventID:       eventID,
		Type:          TypeAnalysisRequested,
		Version:       EnvelopeVersion,
		OccurredAt:    occurredAt.UTC().Format(time.RFC3339),
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
