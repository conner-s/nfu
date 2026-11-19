package flake

import (
	"fmt"
	"strings"
)

// IsInputName reports whether name is one `nix flake update` takes: ASCII
// letters, digits, "_" and "-", starting with a letter
// ([a-zA-Z][a-zA-Z0-9_-]*, flakeIdRegexS in nix's libflake, unchanged from
// nix 2.19 to 2.35).
//
// A flake.lock can hold any string as an input's name: nix itself locks an
// input written as inputs."--refresh" in flake.nix. Such an input cannot be
// updated by name. Given `--refresh` where a name goes, nix reads an option,
// and `nix flake update --refresh` updates every input of the flake.
func IsInputName(name string) bool {
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case i > 0 && (c >= '0' && c <= '9' || c == '_' || c == '-'):
		default:
			return false
		}
	}
	return name != ""
}

// InvalidInputError is returned for names that are not input names by nix's
// rule (IsInputName). nfu never puts such a name on nix's command line.
type InvalidInputError struct {
	Names []string
}

func (e *InvalidInputError) Error() string {
	quoted := make([]string, len(e.Names))
	for i, n := range e.Names {
		quoted[i] = fmt.Sprintf("%q", n)
	}
	verb := "is not a name"
	if len(quoted) > 1 {
		verb = "are not names"
	}
	return fmt.Sprintf("%s %s nix can update: an input's name is ASCII letters, digits, \"_\" and \"-\", and starts with a letter",
		strings.Join(quoted, ", "), verb)
}

// CheckInputNames returns an InvalidInputError naming those of names that are
// not input names by nix's rule, or nil. Everything that is about to put a
// name on nix's command line calls it first, so the rule does not rest on any
// one caller.
func CheckInputNames(names ...string) error {
	var bad []string
	for _, n := range names {
		if !IsInputName(n) {
			bad = append(bad, n)
		}
	}
	if len(bad) > 0 {
		return &InvalidInputError{Names: bad}
	}
	return nil
}
