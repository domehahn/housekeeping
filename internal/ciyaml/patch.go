// Package ciyaml implements a narrow, deliberately conservative patcher
// for adding a CI runner tag to a GitLab CI YAML document
// (.gitlab-ci.yml). It has no I/O and no GitLab API knowledge - it is a
// pure function of (content, tag) -> (patched content, changes), tested
// the same way internal/policy is: table-driven, no network, no adapter
// involved. The GitLab-specific parts (fetching the file, opening a
// Merge Request with the result) live in internal/adapters/gitlab.
//
// Scope (see docs/adr/0005-ci-tag-management-scope.md for the full
// rationale):
//
//   - The document-wide `default: tags:` block is always ensured to
//     contain the tag (created if missing).
//   - Any *existing* per-job `tags:` list (including hidden "."-prefixed
//     template jobs, so the tag reaches anything that `extends` them) is
//     also given the tag, since a job that already overrides `default:`
//     would otherwise never see it.
//   - A job with no `tags:` key of its own is deliberately left alone -
//     it already inherits from `default:`, and inventing a `tags:` key
//     for it would change how GitLab schedules that job in ways nothing
//     asked for (e.g. interaction with `run_untagged`).
//   - Content reachable only through `include:` (other files, other
//     projects) is never touched - HasIncludes reports when a document
//     has one, so callers can surface it as a "may be incomplete"
//     warning rather than silently missing jobs.
package ciyaml

import (
	"bytes"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

// ChangeKind classifies a single edit AddTag made.
type ChangeKind string

const (
	// ChangeDefaultCreated means the document had no default: block (or
	// no default.tags) and one was created with the tag.
	ChangeDefaultCreated ChangeKind = "default_created"
	// ChangeJobAppended means an existing job (or hidden template) already
	// had its own tags: list, and the tag was appended to it.
	ChangeJobAppended ChangeKind = "job_appended"
)

// Change describes one edit made by AddTag. Job is empty for the
// default: block.
type Change struct {
	Kind ChangeKind
	Job  string
}

// reservedTopLevelKeys are GitLab CI keywords that are never job
// definitions, so AddTag never treats them as one.
var reservedTopLevelKeys = map[string]bool{
	"default":       true,
	"include":       true,
	"image":         true,
	"services":      true,
	"spec":          true,
	"stages":        true,
	"variables":     true,
	"workflow":      true,
	"cache":         true,
	"before_script": true,
	"after_script":  true,
}

// AddTag ensures tag is present in the document's default: tags: block
// and in every existing job-level tags: block, returning the patched
// content and a list of what changed.
//
// If the tag is already present everywhere it would otherwise be added,
// AddTag returns the input content byte-for-byte unchanged and a nil
// Changes slice - callers use len(changes) == 0 to detect this idempotent
// no-op case rather than a separate sentinel value, so a re-run never
// produces a spurious diff.
func AddTag(content []byte, tag string) (patched []byte, changes []Change, err error) {
	if tag == "" {
		return nil, nil, fmt.Errorf("ciyaml: tag must not be empty")
	}

	docs, configDoc, err := decodeGitLabDocuments(content)
	if err != nil {
		return nil, nil, err
	}
	root, err := rootMapping(configDoc)
	if err != nil {
		return nil, nil, err
	}

	var allChanges []Change

	changed, err := ensureDefaultTag(root, tag)
	if err != nil {
		return nil, nil, err
	}
	if changed {
		allChanges = append(allChanges, Change{Kind: ChangeDefaultCreated})
	}

	jobChanges, err := ensureJobTags(root, tag)
	if err != nil {
		return nil, nil, err
	}
	allChanges = append(allChanges, jobChanges...)

	if len(allChanges) == 0 {
		return content, nil, nil
	}

	out, err := marshalDocuments(docs)
	if err != nil {
		return nil, nil, fmt.Errorf("ciyaml: encode patched document: %w", err)
	}
	return out, allChanges, nil
}

// HasIncludes reports whether the document has a top-level include: key.
// Best-effort: a document that fails to parse reports false rather than
// erroring, since this is purely an informational signal for callers that
// have already successfully handled (or are about to report) the parse
// error through AddTag.
func HasIncludes(content []byte) bool {
	_, configDoc, err := decodeGitLabDocuments(content)
	if err != nil {
		return false
	}
	root, err := rootMapping(configDoc)
	if err != nil {
		return false
	}
	_, _, found := findMapEntry(root, "include")
	return found
}

// decodeGitLabDocuments accepts both the usual single CI document and
// GitLab's two-document header form (`spec: ...`, `---`, configuration).
// Rejecting any other multi-document shape is intentional: silently choosing
// one document could discard CI configuration when the result is encoded.
func decodeGitLabDocuments(content []byte) (docs []*yaml.Node, configDoc *yaml.Node, err error) {
	dec := yaml.NewDecoder(bytes.NewReader(content))
	for {
		var doc yaml.Node
		if decodeErr := dec.Decode(&doc); decodeErr != nil {
			if decodeErr == io.EOF {
				break
			}
			return nil, nil, fmt.Errorf("ciyaml: parse YAML: %w", decodeErr)
		}
		if len(doc.Content) == 0 {
			continue
		}
		docs = append(docs, &doc)
	}
	if len(docs) == 0 {
		return nil, nil, fmt.Errorf("ciyaml: document is empty")
	}
	if len(docs) == 1 {
		return docs, docs[0], nil
	}
	if len(docs) != 2 {
		return nil, nil, fmt.Errorf("ciyaml: unsupported YAML stream with %d documents", len(docs))
	}
	header, headerErr := rootMapping(docs[0])
	if headerErr != nil {
		return nil, nil, fmt.Errorf("ciyaml: invalid header document: %w", headerErr)
	}
	if _, _, found := findMapEntry(header, "spec"); !found {
		return nil, nil, fmt.Errorf("ciyaml: multiple YAML documents require a leading GitLab spec: header")
	}
	return docs, docs[1], nil
}

// rootMapping returns the document's root mapping node, erroring if the
// document is empty or its root is not a mapping (i.e. not a valid
// GitLab CI YAML document shape).
func rootMapping(doc *yaml.Node) (*yaml.Node, error) {
	if len(doc.Content) == 0 {
		return nil, fmt.Errorf("ciyaml: document is empty")
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("ciyaml: document root is not a mapping (got kind %d) - not a valid GitLab CI YAML document", root.Kind)
	}
	return root, nil
}

// findMapEntry looks for key among a mapping node's flat [key, value, ...]
// Content and returns its key/value nodes and whether it was found.
func findMapEntry(mapping *yaml.Node, key string) (keyNode, valueNode *yaml.Node, found bool) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i], mapping.Content[i+1], true
		}
	}
	return nil, nil, false
}

// ensureDefaultTag makes sure root.default.tags contains tag, creating
// default: and/or default.tags: as needed. Returns whether anything was
// added.
func ensureDefaultTag(root *yaml.Node, tag string) (bool, error) {
	_, defaultVal, found := findMapEntry(root, "default")
	if !found {
		seq := newTagSequence(tag)
		defaultMap := &yaml.Node{
			Kind:    yaml.MappingNode,
			Tag:     "!!map",
			Content: []*yaml.Node{scalarKey("tags"), seq},
		}
		root.Content = append([]*yaml.Node{scalarKey("default"), defaultMap}, root.Content...)
		return true, nil
	}
	if defaultVal.Kind != yaml.MappingNode {
		return false, fmt.Errorf("ciyaml: top-level default: is not a mapping - not a valid GitLab CI YAML document")
	}
	return ensureTagInMapping(defaultVal, tag)
}

// ensureJobTags appends tag to every existing job-level tags: list. A
// "job" is any top-level key that is not a reserved GitLab CI keyword;
// hidden ("."-prefixed) template jobs are included deliberately, since
// GitLab's `extends:` merges their keys (including tags) into whatever
// job extends them.
func ensureJobTags(root *yaml.Node, tag string) ([]Change, error) {
	var changes []Change
	for i := 0; i+1 < len(root.Content); i += 2 {
		keyNode, valNode := root.Content[i], root.Content[i+1]
		if reservedTopLevelKeys[keyNode.Value] {
			continue
		}
		if valNode.Kind != yaml.MappingNode {
			continue // not a job definition (e.g. a scalar/sequence anchor used only via merge keys elsewhere)
		}
		_, tagsVal, found := findMapEntry(valNode, "tags")
		if !found {
			continue // no tags of its own - inherits default:, deliberately left alone
		}
		if tagsVal.Kind != yaml.SequenceNode {
			return nil, fmt.Errorf("ciyaml: job %q has a non-list tags: value - not a valid GitLab CI YAML document", keyNode.Value)
		}
		added, err := appendIfMissing(tagsVal, tag)
		if err != nil {
			return nil, err
		}
		if added {
			changes = append(changes, Change{Kind: ChangeJobAppended, Job: keyNode.Value})
		}
	}
	return changes, nil
}

// ensureTagInMapping ensures mapping has a tags: sequence containing tag,
// creating the tags: key if absent.
func ensureTagInMapping(mapping *yaml.Node, tag string) (bool, error) {
	_, tagsVal, found := findMapEntry(mapping, "tags")
	if !found {
		mapping.Content = append(mapping.Content, scalarKey("tags"), newTagSequence(tag))
		return true, nil
	}
	if tagsVal.Kind != yaml.SequenceNode {
		return false, fmt.Errorf("ciyaml: default.tags is not a list - not a valid GitLab CI YAML document")
	}
	return appendIfMissing(tagsVal, tag)
}

// appendIfMissing appends a scalar tag value to a sequence node if not
// already present among its scalar values.
func appendIfMissing(seq *yaml.Node, tag string) (bool, error) {
	for _, item := range seq.Content {
		if item.Kind == yaml.ScalarNode && item.Value == tag {
			return false, nil
		}
	}
	seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: tag})
	return true, nil
}

func newTagSequence(tag string) *yaml.Node {
	return &yaml.Node{
		Kind:    yaml.SequenceNode,
		Tag:     "!!seq",
		Content: []*yaml.Node{{Kind: yaml.ScalarNode, Value: tag}},
	}
}

func scalarKey(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Value: s}
}

func marshalDocuments(docs []*yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	for _, doc := range docs {
		if err := enc.Encode(doc); err != nil {
			return nil, err
		}
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
