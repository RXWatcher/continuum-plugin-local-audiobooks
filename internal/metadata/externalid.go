package metadata

import (
	"errors"
	"strings"
)

// ErrBadExternalID is returned when an external ID is missing the
// "<source>:<native-id>" prefix.
var ErrBadExternalID = errors.New("external id missing source prefix")

// FormatExternalID joins a source and a native ID into the wire format.
func FormatExternalID(source, nativeID string) string {
	return source + ":" + nativeID
}

// ParseExternalID splits a wire-format external ID. The native ID portion
// may itself contain colons (Storytel uses kebab-case IDs but other sources
// theoretically could); we split on the first colon only.
func ParseExternalID(id string) (source, nativeID string, err error) {
	i := strings.IndexByte(id, ':')
	if i <= 0 || i == len(id)-1 {
		return "", "", ErrBadExternalID
	}
	return id[:i], id[i+1:], nil
}
