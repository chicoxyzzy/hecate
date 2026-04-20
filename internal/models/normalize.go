package models

import "strings"

type Identity struct {
	Requested          string
	CanonicalRequested string
	Resolved           string
	CanonicalResolved  string
}

func Canonicalize(model string) string {
	parts := strings.Split(model, "-")
	if len(parts) < 4 {
		return model
	}

	lastThree := parts[len(parts)-3:]
	for _, part := range lastThree {
		if len(part) != 2 && len(part) != 4 {
			return model
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return model
			}
		}
	}

	return strings.Join(parts[:len(parts)-3], "-")
}

func BuildIdentity(requested, resolved string) Identity {
	return Identity{
		Requested:          requested,
		CanonicalRequested: Canonicalize(requested),
		Resolved:           resolved,
		CanonicalResolved:  Canonicalize(resolved),
	}
}
