package flake

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// metadata is the slice of `nix flake metadata --json` nfu reads: the lock
// graph. The root node's `inputs` map names each top-level input; the referenced
// nodes carry the locked pins.
type metadata struct {
	Locks struct {
		Root  string              `json:"root"`
		Nodes map[string]lockNode `json:"nodes"`
	} `json:"locks"`
}

// lockNode is one node in the lock graph. An input edge is either a node key
// (string) or a follows path from the root (array of input names).
type lockNode struct {
	Inputs map[string]json.RawMessage `json:"inputs"`
	Locked lockedSource               `json:"locked"`
}

// lockedSource is the `locked` object of a node — the concrete pin.
type lockedSource struct {
	Type         string `json:"type"`
	URL          string `json:"url"`
	Ref          string `json:"ref"`
	Rev          string `json:"rev"`
	NarHash      string `json:"narHash"`
	LastModified int64  `json:"lastModified"`
	Owner        string `json:"owner"`
	Repo         string `json:"repo"`
}

// displayURL yields a human source string. `git`/`tarball`/`path` carry a real
// url; `github`/`gitlab` carry owner/repo instead, so synthesize one.
func (l lockedSource) displayURL() string {
	if l.URL != "" {
		return l.URL
	}
	if l.Owner != "" && l.Repo != "" {
		return fmt.Sprintf("%s:%s/%s", l.Type, l.Owner, l.Repo)
	}
	return ""
}

func (m *metadata) rootKey() string {
	if m.Locks.Root != "" {
		return m.Locks.Root
	}
	return "root"
}

// rootInputs returns the raw input edges of the root node.
func (m *metadata) rootInputs() (map[string]json.RawMessage, error) {
	root, ok := m.Locks.Nodes[m.rootKey()]
	if !ok {
		return nil, fmt.Errorf("flake lock: root node %q missing", m.rootKey())
	}
	return root.Inputs, nil
}

// nodeKey resolves the lock-graph node for the root input named input via
// locks.nodes[root].inputs. Node keys are NOT input names: a second nixpkgs
// copy yields keys like "nixpkgs_2", so a direct Nodes[input] lookup can hit a
// transitive input's node and report the wrong rev. Follows edges (arrays) are
// walked from the root.
func (m *metadata) nodeKey(input string) (string, error) {
	root := m.rootKey()
	key := root
	path := []string{input}
	for steps := 0; len(path) > 0; steps++ {
		if steps > 100 {
			return "", fmt.Errorf("flake lock: follows cycle resolving input %q", input)
		}
		node, ok := m.Locks.Nodes[key]
		if !ok {
			return "", fmt.Errorf("flake lock: node %q missing", key)
		}
		raw, ok := node.Inputs[path[0]]
		if !ok {
			return "", fmt.Errorf("flake lock: input %q not found under node %q", path[0], key)
		}
		var next string
		if err := json.Unmarshal(raw, &next); err == nil {
			key = next
			path = path[1:]
			continue
		}
		// Follows edge: an absolute input path from the root node.
		var follows []string
		if err := json.Unmarshal(raw, &follows); err != nil {
			return "", fmt.Errorf("flake lock: malformed input edge %q on node %q", path[0], key)
		}
		path = append(follows, path[1:]...)
		key = root
	}
	return key, nil
}

// isFollows reports whether a root input edge is a `follows` alias (an array)
// rather than a node key (a string).
func isFollows(raw json.RawMessage) bool {
	var s string
	return json.Unmarshal(raw, &s) != nil
}

// Metadata runs `nix flake metadata --json` and parses the lock graph.
//
// --no-write-lock-file is essential: without it, reading metadata on a flake
// whose flake.nix has an input not yet in flake.lock makes nix WRITE that input
// into the lock as a side effect of the read. nfu's "before" snapshot must never
// mutate flake.lock — only `nix flake update` (which omits this flag) may.
func (c *Client) Metadata(ctx context.Context) (*metadata, error) {
	res, err := c.run(ctx, "nix", nixArgs("flake", "metadata", "--json", "--no-write-lock-file")...)
	if err != nil {
		return nil, fmt.Errorf("flake metadata: %w", err)
	}
	var md metadata
	if err := json.Unmarshal(res.stdout, &md); err != nil {
		return nil, fmt.Errorf("parse flake metadata: %w", err)
	}
	return &md, nil
}

// Inputs lists the flake's top-level inputs, name-sorted, each with its locked
// pin. A `follows` alias is marked (its pin reflects the input it follows).
func (c *Client) Inputs(ctx context.Context) ([]Input, error) {
	md, err := c.Metadata(ctx)
	if err != nil {
		return nil, err
	}
	return md.buildInputs()
}

// buildInputs is the pure lock→[]Input mapping, split out so it can be tested
// against fixture metadata without shelling out to nix.
func (m *metadata) buildInputs() ([]Input, error) {
	edges, err := m.rootInputs()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(edges))
	for name := range edges {
		names = append(names, name)
	}
	sort.Strings(names)

	inputs := make([]Input, 0, len(names))
	for _, name := range names {
		in := Input{Name: name, Follows: isFollows(edges[name])}
		if key, err := m.nodeKey(name); err == nil {
			if node, ok := m.Locks.Nodes[key]; ok {
				l := node.Locked
				in.Type = l.Type
				in.URL = l.displayURL()
				in.Ref = l.Ref
				in.Rev = l.Rev
				in.NarHash = l.NarHash
				in.LastModified = l.LastModified
			}
		}
		inputs = append(inputs, in)
	}
	return inputs, nil
}

// InputInfo returns one input's current pin, or an error listing the available
// inputs when name is not a top-level input.
func (c *Client) InputInfo(ctx context.Context, name string) (Input, error) {
	inputs, err := c.Inputs(ctx)
	if err != nil {
		return Input{}, err
	}
	for _, in := range inputs {
		if in.Name == name {
			return in, nil
		}
	}
	return Input{}, &UnknownInputError{Name: name, Available: inputNames(inputs)}
}

func inputNames(inputs []Input) []string {
	names := make([]string, len(inputs))
	for i, in := range inputs {
		names[i] = in.Name
	}
	return names
}

// UnknownInputError is returned when a requested input is not a top-level input
// of the flake. It lists what is available so the user can correct a typo.
type UnknownInputError struct {
	Name      string
	Available []string
}

func (e *UnknownInputError) Error() string {
	if len(e.Available) > 0 {
		return fmt.Sprintf("no flake input named %q\n  available: %s", e.Name, strings.Join(e.Available, ", "))
	}
	return fmt.Sprintf("no flake input named %q", e.Name)
}
