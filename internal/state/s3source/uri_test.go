package s3source

import "testing"

func TestParseURI(t *testing.T) {
	cases := []struct {
		name       string
		uri        string
		wantBucket string
		wantPrefix string
		wantErr    bool
	}{
		{"bucket only", "s3://my-bucket", "my-bucket", "", false},
		{"bucket trailing slash", "s3://my-bucket/", "my-bucket", "", false},
		{"bucket with prefix", "s3://my-bucket/states/prod", "my-bucket", "states/prod", false},
		{"bucket with deep prefix and trailing slash", "s3://my-bucket/envs/prod/network/", "my-bucket", "envs/prod/network/", false},
		{"missing scheme", "my-bucket/states", "", "", true},
		{"empty", "", "", "", true},
		{"scheme only", "s3://", "", "", true},
		{"empty bucket with slash", "s3:///states", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bucket, prefix, err := ParseURI(tc.uri)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if bucket != tc.wantBucket {
				t.Errorf("bucket = %q, want %q", bucket, tc.wantBucket)
			}
			if prefix != tc.wantPrefix {
				t.Errorf("prefix = %q, want %q", prefix, tc.wantPrefix)
			}
		})
	}
}
