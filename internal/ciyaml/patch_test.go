package ciyaml

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/domehahn/housekeeping/internal/domain"
)

// mustParse decodes into a generic map for assertions that don't care
// about formatting/comments, only structure.
func mustParse(t *testing.T, content []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := yaml.Unmarshal(content, &m); err != nil {
		t.Fatalf("failed to parse result as YAML: %v\n---\n%s", err, content)
	}
	return m
}

func tagsOf(t *testing.T, m map[string]any, path ...string) []string {
	t.Helper()
	cur := m
	for i, p := range path {
		if i == len(path)-1 {
			raw, ok := cur[p]
			if !ok {
				return nil
			}
			list, ok := raw.([]any)
			if !ok {
				t.Fatalf("expected %v to be a list, got %T", path, raw)
			}
			out := make([]string, len(list))
			for i, v := range list {
				out[i] = v.(string)
			}
			return out
		}
		next, ok := cur[p].(map[string]any)
		if !ok {
			return nil
		}
		cur = next
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func TestAddTag_CreatesDefaultBlockWhenMissing(t *testing.T) {
	content := []byte("stages:\n  - build\n\nbuild-job:\n  script: [\"echo hi\"]\n")

	patched, changes, err := AddTag(content, "k8s-runner")
	if err != nil {
		t.Fatalf("AddTag: %v", err)
	}
	if len(changes) != 1 || changes[0].Kind != ChangeDefaultCreated {
		t.Fatalf("expected exactly one default_created change, got %+v", changes)
	}

	m := mustParse(t, patched)
	if got := tagsOf(t, m, "default", "tags"); !contains(got, "k8s-runner") {
		t.Errorf("expected default.tags to contain k8s-runner, got %v", got)
	}
	if got := tagsOf(t, m, "stages"); len(got) != 1 || got[0] != "build" {
		t.Errorf("expected stages to be left untouched, got %v", got)
	}
}

func TestAddTag_AppendsToExistingDefaultTags(t *testing.T) {
	content := []byte("default:\n  tags:\n    - existing-tag\n\nbuild-job:\n  script: [\"echo hi\"]\n")

	patched, changes, err := AddTag(content, "k8s-runner")
	if err != nil {
		t.Fatalf("AddTag: %v", err)
	}
	if len(changes) != 1 || changes[0].Kind != ChangeDefaultCreated {
		t.Fatalf("expected one change (still reported as default_created for the default block), got %+v", changes)
	}

	m := mustParse(t, patched)
	got := tagsOf(t, m, "default", "tags")
	if !contains(got, "existing-tag") || !contains(got, "k8s-runner") {
		t.Errorf("expected both existing-tag and k8s-runner in default.tags, got %v", got)
	}
}

func TestAddTag_IdempotentWhenTagAlreadyInDefault(t *testing.T) {
	content := []byte("default:\n  tags:\n    - k8s-runner\n\nbuild-job:\n  script: [\"echo hi\"]\n")

	patched, changes, err := AddTag(content, "k8s-runner")
	if err != nil {
		t.Fatalf("AddTag: %v", err)
	}
	if len(changes) != 0 {
		t.Errorf("expected no changes when tag is already present, got %+v", changes)
	}
	if string(patched) != string(content) {
		t.Errorf("expected byte-identical output on a no-op, got:\n%s", patched)
	}
}

func TestAddTag_AppendsToExistingJobTags(t *testing.T) {
	content := []byte(`default:
  tags:
    - shared-tag

test-job:
  tags:
    - job-specific-tag
  script:
    - echo test

deploy-job:
  script:
    - echo deploy
`)

	patched, changes, err := AddTag(content, "k8s-runner")
	if err != nil {
		t.Fatalf("AddTag: %v", err)
	}

	var jobChange *Change
	for i := range changes {
		if changes[i].Kind == ChangeJobAppended {
			jobChange = &changes[i]
		}
	}
	if jobChange == nil || jobChange.Job != "test-job" {
		t.Fatalf("expected a job_appended change for test-job, got %+v", changes)
	}

	m := mustParse(t, patched)
	if got := tagsOf(t, m, "test-job", "tags"); !contains(got, "job-specific-tag") || !contains(got, "k8s-runner") {
		t.Errorf("expected test-job.tags to contain both tags, got %v", got)
	}
	if got := tagsOf(t, m, "deploy-job", "tags"); got != nil {
		t.Errorf("expected deploy-job to remain without its own tags: key (inherits default), got %v", got)
	}
}

func TestAddTag_HiddenTemplateJobGetsAppended(t *testing.T) {
	content := []byte(`.template:
  tags:
    - template-tag
  script:
    - echo base

real-job:
  extends: .template
`)

	patched, changes, err := AddTag(content, "k8s-runner")
	if err != nil {
		t.Fatalf("AddTag: %v", err)
	}

	found := false
	for _, c := range changes {
		if c.Kind == ChangeJobAppended && c.Job == ".template" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected .template's tags to be patched (extends picks it up at compile time), got %+v", changes)
	}

	m := mustParse(t, patched)
	if got := tagsOf(t, m, ".template", "tags"); !contains(got, "k8s-runner") {
		t.Errorf("expected .template.tags to contain k8s-runner, got %v", got)
	}
}

func TestAddTag_ReservedKeywordsNeverTreatedAsJobs(t *testing.T) {
	content := []byte(`variables:
  tags:
    - not-a-job-tag-list

stages:
  - build

image: alpine

build-job:
  script: ["echo hi"]
`)

	patched, _, err := AddTag(content, "k8s-runner")
	if err != nil {
		t.Fatalf("AddTag: %v", err)
	}

	m := mustParse(t, patched)
	// variables.tags looks like a job tags: list but variables is reserved -
	// it must never be touched, even though it happens to have a key
	// named "tags".
	if got := tagsOf(t, m, "variables", "tags"); contains(got, "k8s-runner") {
		t.Errorf("variables.tags must never be treated as a job tags list, got %v", got)
	}
}

func TestAddTag_AnchorAliasSharesPatchedTags(t *testing.T) {
	content := []byte(`.base: &base
  tags:
    - base-tag
  script:
    - echo base

job-a:
  <<: *base

job-b: *base
`)

	patched, changes, err := AddTag(content, "k8s-runner")
	if err != nil {
		t.Fatalf("AddTag: %v", err)
	}
	found := false
	for _, c := range changes {
		if c.Job == ".base" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected .base (the anchor owner) to be patched, got %+v", changes)
	}

	m := mustParse(t, patched)
	if got := tagsOf(t, m, ".base", "tags"); !contains(got, "k8s-runner") {
		t.Errorf("expected .base.tags to contain k8s-runner, got %v", got)
	}
	// job-b is a plain alias to the same node - decoding it independently
	// must show the same patched tags, proving the shared node was edited
	// once and reflected everywhere it's referenced.
	if got := tagsOf(t, m, "job-b", "tags"); !contains(got, "k8s-runner") {
		t.Errorf("expected job-b (alias of .base) to reflect the patched tags, got %v", got)
	}
}

func TestAddTag_MalformedYAMLReturnsError(t *testing.T) {
	_, _, err := AddTag([]byte("not: valid: yaml: [broken"), "k8s-runner")
	if err == nil {
		t.Fatal("expected an error for malformed YAML")
	}
}

func TestAddTag_NonMappingRootReturnsError(t *testing.T) {
	_, _, err := AddTag([]byte("- just\n- a\n- list\n"), "k8s-runner")
	if err == nil {
		t.Fatal("expected an error for a non-mapping root document")
	}
}

func TestAddTag_EmptyDocumentReturnsError(t *testing.T) {
	_, _, err := AddTag([]byte(""), "k8s-runner")
	if err == nil {
		t.Fatal("expected an error for an empty document")
	}
}

func TestAddTag_EmptyTagIsRejected(t *testing.T) {
	_, _, err := AddTag([]byte("build-job:\n  script: [\"x\"]\n"), "")
	if err == nil {
		t.Fatal("expected an error for an empty tag value")
	}
}

func TestAddTags_AddsMultipleWithoutReplacingExisting(t *testing.T) {
	content := []byte("default:\n  tags: [docker]\nbuild:\n  tags: [linux]\n")
	patched, changes, err := AddTags(content, []string{"AKS", "production", "AKS"})
	if err != nil {
		t.Fatal(err)
	}
	got := string(patched)
	for _, want := range []string{"docker", "linux", "AKS", "production"} {
		if !strings.Contains(got, want) {
			t.Fatalf("patched YAML missing %q:\n%s", want, got)
		}
	}
	if len(changes) != 4 {
		t.Fatalf("changes = %+v, want two default and two job additions", changes)
	}
}

func TestAddTag_SecondCallIsFullyIdempotent(t *testing.T) {
	content := []byte(`test-job:
  tags:
    - existing
  script:
    - echo test
`)

	first, changes1, err := AddTag(content, "k8s-runner")
	if err != nil {
		t.Fatalf("first AddTag: %v", err)
	}
	if len(changes1) == 0 {
		t.Fatal("expected the first call to make changes")
	}

	second, changes2, err := AddTag(first, "k8s-runner")
	if err != nil {
		t.Fatalf("second AddTag: %v", err)
	}
	if len(changes2) != 0 {
		t.Errorf("expected the second call to be a no-op, got %+v", changes2)
	}
	if string(second) != string(first) {
		t.Error("expected the second call's output to be byte-identical to the first call's output")
	}
}

func TestAddTag_PreservesGitLabSpecHeaderAndPatchesConfigurationDocument(t *testing.T) {
	content := []byte(`spec:
  inputs:
    environment:
      default: test
---
build-job:
  tags:
    - existing
  script: ["echo hi"]
`)

	patched, changes, err := AddTag(content, "k8s-runner")
	if err != nil {
		t.Fatalf("AddTag: %v", err)
	}
	if len(changes) != 2 {
		t.Fatalf("expected default and job changes, got %+v", changes)
	}

	dec := yaml.NewDecoder(bytes.NewReader(patched))
	var header, config map[string]any
	if err := dec.Decode(&header); err != nil {
		t.Fatalf("decode header: %v", err)
	}
	if err := dec.Decode(&config); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		t.Fatalf("expected exactly two documents, got %v", err)
	}
	if _, ok := header["spec"]; !ok {
		t.Fatalf("spec header was lost: %#v", header)
	}
	if got := tagsOf(t, config, "default", "tags"); !contains(got, "k8s-runner") {
		t.Errorf("expected config default.tags to be patched, got %v", got)
	}
	if got := tagsOf(t, config, "build-job", "tags"); !contains(got, "k8s-runner") {
		t.Errorf("expected build-job.tags to be patched, got %v", got)
	}

	second, secondChanges, err := AddTag(patched, "k8s-runner")
	if err != nil {
		t.Fatalf("second AddTag: %v", err)
	}
	if len(secondChanges) != 0 || !bytes.Equal(second, patched) {
		t.Fatal("expected the second multi-document patch to be byte-identical")
	}
}

func TestAddTag_RejectsUnrecognizedMultiDocumentStream(t *testing.T) {
	_, _, err := AddTag([]byte("one: value\n---\ntwo: value\n"), "k8s-runner")
	if err == nil || !strings.Contains(err.Error(), "spec") {
		t.Fatalf("expected a spec-header error, got %v", err)
	}
}

func TestHasIncludes_InGitLabConfigurationDocument(t *testing.T) {
	content := []byte("spec:\n  inputs: {}\n---\ninclude:\n  - local: common.yml\n")
	if !HasIncludes(content) {
		t.Fatal("expected include in the configuration document to be detected")
	}
}

func TestHasIncludes(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"no include", "build-job:\n  script: [\"x\"]\n", false},
		{"has include", "include:\n  - project: foo/bar\n    file: .gitlab-ci-common.yml\nbuild-job:\n  script: [\"x\"]\n", true},
		{"malformed yaml", "not: valid: [", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasIncludes([]byte(tc.content)); got != tc.want {
				t.Errorf("HasIncludes(%q) = %v, want %v", tc.content, got, tc.want)
			}
		})
	}
}

func TestAddTag_PreservesUnrelatedComments(t *testing.T) {
	content := []byte(`# This pipeline builds and deploys the service.
stages:
  - build

build-job:
  script:
    - echo hi
`)
	patched, _, err := AddTag(content, "k8s-runner")
	if err != nil {
		t.Fatalf("AddTag: %v", err)
	}
	if !strings.Contains(string(patched), "# This pipeline builds and deploys the service.") {
		t.Errorf("expected the leading comment to survive patching, got:\n%s", patched)
	}
}

func TestReplaceTag_ReplacesInDefaultTags(t *testing.T) {
	content := []byte("default:\n  tags:\n    - AKS\n    - other-tag\n\nbuild-job:\n  script: [\"echo hi\"]\n")

	patched, changes, err := ReplaceTag(content, "AKS", "aks")
	if err != nil {
		t.Fatalf("ReplaceTag: %v", err)
	}
	if len(changes) != 1 || changes[0].Kind != ChangeDefaultReplaced || changes[0].OldTag != "AKS" || changes[0].Tag != "aks" {
		t.Fatalf("expected one default_replaced change AKS->aks, got %+v", changes)
	}

	m := mustParse(t, patched)
	got := tagsOf(t, m, "default", "tags")
	if contains(got, "AKS") {
		t.Errorf("expected AKS to be removed from default.tags, got %v", got)
	}
	if !contains(got, "aks") || !contains(got, "other-tag") {
		t.Errorf("expected aks and other-tag in default.tags, got %v", got)
	}
}

func TestReplaceTag_ReplacesInJobTags(t *testing.T) {
	content := []byte(`test-job:
  tags:
    - AKS
  script:
    - echo test

deploy-job:
  script:
    - echo deploy
`)
	patched, changes, err := ReplaceTag(content, "AKS", "aks")
	if err != nil {
		t.Fatalf("ReplaceTag: %v", err)
	}
	var jobChange *Change
	for i := range changes {
		if changes[i].Kind == ChangeJobReplaced {
			jobChange = &changes[i]
		}
	}
	if jobChange == nil || jobChange.Job != "test-job" || jobChange.OldTag != "AKS" || jobChange.Tag != "aks" {
		t.Fatalf("expected a job_replaced change for test-job, got %+v", changes)
	}

	m := mustParse(t, patched)
	if got := tagsOf(t, m, "test-job", "tags"); contains(got, "AKS") || !contains(got, "aks") {
		t.Errorf("expected test-job.tags to have aks and not AKS, got %v", got)
	}
	if got := tagsOf(t, m, "deploy-job", "tags"); got != nil {
		t.Errorf("expected deploy-job to remain untouched (never had tags:), got %v", got)
	}
}

func TestReplaceTag_LeavesUntouchedWhatNeverHadOldTag(t *testing.T) {
	// Unlike AddTag, ReplaceTag must never create a default: block and
	// must never touch a job's tags: list that never had the old tag.
	content := []byte(`test-job:
  tags:
    - some-other-tag
  script:
    - echo test
`)
	patched, changes, err := ReplaceTag(content, "AKS", "aks")
	if err != nil {
		t.Fatalf("ReplaceTag: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("expected no changes, got %+v", changes)
	}
	if string(patched) != string(content) {
		t.Errorf("expected byte-identical output when old tag is absent, got:\n%s", patched)
	}
}

func TestReplaceTag_NoOpWhenOldTagAbsentAnywhere(t *testing.T) {
	content := []byte("default:\n  tags:\n    - aks\n\nbuild-job:\n  script: [\"echo hi\"]\n")
	patched, changes, err := ReplaceTag(content, "AKS", "aks")
	if err != nil {
		t.Fatalf("ReplaceTag: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("expected no changes when old tag is already gone, got %+v", changes)
	}
	if string(patched) != string(content) {
		t.Errorf("expected byte-identical output, got:\n%s", patched)
	}
}

func TestReplaceTag_CaseSensitivity(t *testing.T) {
	// The bug this feature fixes: "AKS" and "aks" are distinct GitLab
	// tags. A document that already has "aks" (lowercase) must not be
	// seen as already correct when asked to replace "AKS" - and vice
	// versa, replacing "AKS" with "aks" must not touch an unrelated,
	// already-lowercase tag.
	content := []byte("default:\n  tags:\n    - AKS\n    - aks\n\nbuild-job:\n  script: [\"echo hi\"]\n")
	patched, changes, err := ReplaceTag(content, "AKS", "aks")
	if err != nil {
		t.Fatalf("ReplaceTag: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("expected exactly one change, got %+v", changes)
	}
	m := mustParse(t, patched)
	got := tagsOf(t, m, "default", "tags")
	if contains(got, "AKS") {
		t.Errorf("expected AKS to be removed, got %v", got)
	}
	count := 0
	for _, tag := range got {
		if tag == "aks" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly one aks entry (no duplicate), got %v", got)
	}
}

func TestReplaceTag_RejectsEqualOldAndNew(t *testing.T) {
	if _, _, err := ReplaceTag([]byte("default:\n  tags: [aks]\n"), "aks", "aks"); err == nil {
		t.Fatal("expected an error when old and new tag are identical")
	}
}

func TestReplaceTag_RejectsEmptyOldOrNew(t *testing.T) {
	content := []byte("default:\n  tags: [aks]\n")
	if _, _, err := ReplaceTag(content, "", "aks"); err == nil {
		t.Fatal("expected an error for empty old tag")
	}
	if _, _, err := ReplaceTag(content, "aks", ""); err == nil {
		t.Fatal("expected an error for empty new tag")
	}
}

func TestReplaceTags_AppliesMultipleRenamesAtomically(t *testing.T) {
	content := []byte("default:\n  tags:\n    - AKS\n    - EKS\n")
	patched, changes, err := ReplaceTags(content, []domain.TagRename{
		{Old: "AKS", New: "aks"},
		{Old: "EKS", New: "eks"},
	})
	if err != nil {
		t.Fatalf("ReplaceTags: %v", err)
	}
	if len(changes) != 2 {
		t.Fatalf("expected two changes, got %+v", changes)
	}
	m := mustParse(t, patched)
	got := tagsOf(t, m, "default", "tags")
	if !contains(got, "aks") || !contains(got, "eks") || contains(got, "AKS") || contains(got, "EKS") {
		t.Errorf("expected both renames applied, got %v", got)
	}
}

func TestReplaceTag_PreservesUnrelatedComments(t *testing.T) {
	content := []byte(`# This pipeline builds and deploys the service.
default:
  tags:
    - AKS

build-job:
  script:
    - echo hi
`)
	patched, _, err := ReplaceTag(content, "AKS", "aks")
	if err != nil {
		t.Fatalf("ReplaceTag: %v", err)
	}
	if !strings.Contains(string(patched), "# This pipeline builds and deploys the service.") {
		t.Errorf("expected the leading comment to survive patching, got:\n%s", patched)
	}
}
