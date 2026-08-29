package domain

// PipelineInclude identifies a source incorporated into GitLab's effective CI
// configuration. It intentionally carries metadata only, never file content.
type PipelineInclude struct {
	Type           string `json:"type" yaml:"type"`
	Location       string `json:"location" yaml:"location"`
	ContextProject string `json:"contextProject,omitempty" yaml:"context_project,omitempty"`
	ContextSHA     string `json:"contextSha,omitempty" yaml:"context_sha,omitempty"`
}

// PipelineProposal is the provider-independent status of one scm-cleaner CI
// tag Merge Request.
type PipelineProposal struct {
	ProjectID    string `json:"projectId" yaml:"project_id"`
	ProjectPath  string `json:"projectPath" yaml:"project_path"`
	Title        string `json:"title" yaml:"title"`
	State        string `json:"state" yaml:"state"`
	SourceBranch string `json:"sourceBranch" yaml:"source_branch"`
	URL          string `json:"url" yaml:"url"`
}
