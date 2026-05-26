package s3source

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"drifthound/internal/state"
)

// Source lists every state-shaped object under bucket/prefix and exposes each
// as a state.Ref. Filtering is suffix-based (.tfstate / .json) — coarse on
// purpose; tighten it once we have a config layer.
type Source struct {
	Client *s3.Client
	Bucket string
	Prefix string

	// Debugf, when set, receives the s3:// URI of every list/get the source
	// performs. CLI wires it via --debug. nil = silent.
	Debugf func(format string, args ...any)
}

func (s *Source) debugf(format string, args ...any) {
	if s.Debugf != nil {
		s.Debugf(format, args...)
	}
}

// New builds a Source from an aws.Config so callers don't have to import the
// S3 SDK directly.
func New(cfg aws.Config, bucket, prefix string) *Source {
	return &Source{
		Client: s3.NewFromConfig(cfg),
		Bucket: bucket,
		Prefix: prefix,
	}
}

func (s *Source) Discover(ctx context.Context) ([]state.Ref, error) {
	var refs []state.Ref
	var token *string
	for {
		s.debugf("s3: list s3://%s/%s", s.Bucket, s.Prefix)
		out, err := s.Client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            &s.Bucket,
			Prefix:            &s.Prefix,
			ContinuationToken: token,
		})
		if err != nil {
			return nil, fmt.Errorf("list s3://%s/%s: %w", s.Bucket, s.Prefix, err)
		}
		for _, obj := range out.Contents {
			if obj.Key == nil || !looksLikeState(*obj.Key) {
				continue
			}
			refs = append(refs, state.Ref{
				Name: fmt.Sprintf("s3://%s/%s", s.Bucket, *obj.Key),
				Loader: &objectLoader{
					client: s.Client,
					bucket: s.Bucket,
					key:    *obj.Key,
					debugf: s.debugf,
				},
			})
		}
		if out.IsTruncated == nil || !*out.IsTruncated {
			break
		}
		token = out.NextContinuationToken
	}
	return refs, nil
}

func looksLikeState(key string) bool {
	return strings.HasSuffix(key, ".tfstate") || strings.HasSuffix(key, ".tfstate.json") || strings.HasSuffix(key, ".json")
}

type objectLoader struct {
	client *s3.Client
	bucket string
	key    string
	debugf func(format string, args ...any)
}

func (l *objectLoader) Load(ctx context.Context) (io.ReadCloser, error) {
	if l.debugf != nil {
		l.debugf("s3: get s3://%s/%s", l.bucket, l.key)
	}
	out, err := l.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &l.bucket,
		Key:    &l.key,
	})
	if err != nil {
		return nil, fmt.Errorf("get s3://%s/%s: %w", l.bucket, l.key, err)
	}
	return out.Body, nil
}
