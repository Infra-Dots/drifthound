package s3source

import (
	"fmt"
	"strings"
)

// ParseURI splits an s3://bucket/prefix URI into its components. The prefix is
// optional and returned as the empty string when absent. A trailing slash is
// preserved — callers can pass it as-is to ListObjectsV2.
func ParseURI(uri string) (bucket, prefix string, err error) {
	if !strings.HasPrefix(uri, "s3://") {
		return "", "", fmt.Errorf("expected s3://bucket/prefix, got %q", uri)
	}
	rest := strings.TrimPrefix(uri, "s3://")
	if rest == "" {
		return "", "", fmt.Errorf("missing bucket in %q", uri)
	}
	parts := strings.SplitN(rest, "/", 2)
	bucket = parts[0]
	if bucket == "" {
		return "", "", fmt.Errorf("missing bucket in %q", uri)
	}
	if len(parts) == 2 {
		prefix = parts[1]
	}
	return bucket, prefix, nil
}
