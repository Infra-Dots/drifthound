package tfcsource

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"drifthound/internal/state"
)

// Config controls a Terraform Cloud / Enterprise state source. Token and
// Organization are required; an empty Host defaults to app.terraform.io. When
// both Workspaces and Prefixes are empty every workspace in the org is
// included; otherwise the union of (exact name in Workspaces) and (name
// starts with any entry in Prefixes) is selected.
type Config struct {
	Host         string
	Token        string
	Organization string

	Workspaces []string
	Prefixes   []string

	HTTPClient *http.Client

	// Logf, when set, receives progress messages. The CLI wires this to
	// stderr so JSON output on stdout stays clean. nil = silent.
	Logf func(format string, args ...any)

	// Debugf, when set, receives the URLs of every HTTP request made by
	// this source. CLI wires it via --debug. nil = silent.
	Debugf func(format string, args ...any)
}

type Source struct {
	cfg    Config
	client *http.Client
}

func New(cfg Config) (*Source, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("tfc: token is required (--tfc-token or TFC_TOKEN/TFE_TOKEN env)")
	}
	if cfg.Organization == "" {
		return nil, fmt.Errorf("tfc: organization is required (--tfc-org)")
	}
	if cfg.Host == "" {
		cfg.Host = "app.terraform.io"
	}
	client := cfg.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	return &Source{cfg: cfg, client: client}, nil
}

func (s *Source) Discover(ctx context.Context) ([]state.Ref, error) {
	s.logf("tfc: listing workspaces in %s", s.cfg.Organization)
	candidates, err := s.discoverCandidates(ctx)
	if err != nil {
		return nil, err
	}
	// selectWorkspaces enforces strict prefix / exact-match correctness over
	// what the API's substring search returns.
	selected := selectWorkspaces(candidates, s.cfg.Workspaces, s.cfg.Prefixes)
	s.logf("tfc: found %d workspace(s), %d selected after filters", len(candidates), len(selected))

	// Drop workspaces with no current state version — they'd just fail at
	// load time. Caller asked for these to be skipped rather than fatal.
	withState := make([]workspace, 0, len(selected))
	for _, w := range selected {
		if w.DownloadURL == "" {
			s.logf("tfc: skipping %s (no current state version)", w.Name)
			continue
		}
		withState = append(withState, w)
	}

	refs := make([]state.Ref, 0, len(withState))
	for i, w := range withState {
		w := w
		refs = append(refs, state.Ref{
			Name: fmt.Sprintf("tfc:%s/%s", s.cfg.Organization, w.Name),
			Loader: &stateLoader{
				source:        s,
				workspaceName: w.Name,
				downloadURL:   w.DownloadURL,
				index:         i + 1,
				total:         len(withState),
			},
		})
	}
	return refs, nil
}

func (s *Source) logf(format string, args ...any) {
	if s.cfg.Logf != nil {
		s.cfg.Logf(format, args...)
	}
}

func (s *Source) debugf(format string, args ...any) {
	if s.cfg.Debugf != nil {
		s.cfg.Debugf(format, args...)
	}
}

type workspace struct {
	ID          string
	Name        string
	DownloadURL string
}

// discoverCandidates returns the candidate set the API knows about. With no
// filters it's a single full list; with filters it issues one search per
// prefix / exact name and dedupes by workspace ID, mirroring the
// search[name] pattern used by TFC's own list endpoint.
func (s *Source) discoverCandidates(ctx context.Context) ([]workspace, error) {
	if len(s.cfg.Workspaces) == 0 && len(s.cfg.Prefixes) == 0 {
		return s.listWorkspaces(ctx, "")
	}

	seen := make(map[string]workspace)
	for _, term := range s.cfg.Prefixes {
		s.logf("tfc: searching workspaces matching %q", term)
		page, err := s.listWorkspaces(ctx, term)
		if err != nil {
			return nil, err
		}
		for _, w := range page {
			seen[w.ID] = w
		}
	}
	for _, term := range s.cfg.Workspaces {
		s.logf("tfc: searching workspaces matching %q", term)
		page, err := s.listWorkspaces(ctx, term)
		if err != nil {
			return nil, err
		}
		for _, w := range page {
			seen[w.ID] = w
		}
	}

	out := make([]workspace, 0, len(seen))
	for _, w := range seen {
		out = append(out, w)
	}
	return out, nil
}

// listWorkspaces requests workspaces with their current state version inlined
// via include=current_state_version. The state version's hosted-state-download-url
// is read out of the included array and attached to each workspace, so callers
// never have to make a follow-up GET /workspaces/{id}/current-state-version.
func (s *Source) listWorkspaces(ctx context.Context, search string) ([]workspace, error) {
	var out []workspace
	page := 1
	for {
		u := fmt.Sprintf("https://%s/api/v2/organizations/%s/workspaces?page[number]=%d&page[size]=100&include=current_state_version",
			s.cfg.Host, s.cfg.Organization, page)
		if search != "" {
			u += "&search[name]=" + url.QueryEscape(search)
		}
		resp, err := s.doAuthed(ctx, http.MethodGet, u)
		if err != nil {
			return nil, err
		}
		var body struct {
			Data []struct {
				ID         string `json:"id"`
				Attributes struct {
					Name string `json:"name"`
				} `json:"attributes"`
				Relationships struct {
					CurrentStateVersion struct {
						Data *struct {
							ID string `json:"id"`
						} `json:"data"`
					} `json:"current-state-version"`
				} `json:"relationships"`
			} `json:"data"`
			Included []struct {
				ID         string `json:"id"`
				Type       string `json:"type"`
				Attributes struct {
					DownloadURL string `json:"hosted-state-download-url"`
				} `json:"attributes"`
			} `json:"included"`
			Meta struct {
				Pagination struct {
					NextPage *int `json:"next-page"`
				} `json:"pagination"`
			} `json:"meta"`
		}
		err = json.NewDecoder(resp.Body).Decode(&body)
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("tfc: decode workspaces: %w", err)
		}

		urlByStateID := make(map[string]string, len(body.Included))
		for _, inc := range body.Included {
			if inc.Type == "state-versions" {
				urlByStateID[inc.ID] = inc.Attributes.DownloadURL
			}
		}

		for _, d := range body.Data {
			w := workspace{ID: d.ID, Name: d.Attributes.Name}
			if d.Relationships.CurrentStateVersion.Data != nil {
				w.DownloadURL = urlByStateID[d.Relationships.CurrentStateVersion.Data.ID]
			}
			out = append(out, w)
		}
		if body.Meta.Pagination.NextPage == nil {
			return out, nil
		}
		page = *body.Meta.Pagination.NextPage
	}
}

// selectWorkspaces returns the union of workspaces matched by either the
// exact-name set or any of the prefixes. When both filters are empty it
// returns all workspaces unchanged.
func selectWorkspaces(all []workspace, exact, prefixes []string) []workspace {
	if len(exact) == 0 && len(prefixes) == 0 {
		return all
	}
	exactSet := make(map[string]struct{}, len(exact))
	for _, e := range exact {
		exactSet[e] = struct{}{}
	}
	var out []workspace
	for _, w := range all {
		if _, ok := exactSet[w.Name]; ok {
			out = append(out, w)
			continue
		}
		for _, p := range prefixes {
			if strings.HasPrefix(w.Name, p) {
				out = append(out, w)
				break
			}
		}
	}
	return out
}

type stateLoader struct {
	source        *Source
	workspaceName string
	downloadURL   string
	index         int
	total         int
}

func (l *stateLoader) Load(ctx context.Context) (io.ReadCloser, error) {
	l.source.logf("tfc: [%d/%d] loading %s", l.index, l.total, l.workspaceName)
	if l.downloadURL == "" {
		return nil, fmt.Errorf("tfc: workspace %s has no current state version", l.workspaceName)
	}
	return l.download(ctx, l.downloadURL)
}

// download fetches the actual state blob. The archivist URL needs the same
// bearer token as the API — calling it unauthenticated returns 401.
func (l *stateLoader) download(ctx context.Context, url string) (io.ReadCloser, error) {
	l.source.debugf("tfc: GET %s", url)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+l.source.cfg.Token)
	resp, err := l.source.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tfc: download state for %s: %w", l.workspaceName, err)
	}
	if resp.StatusCode >= 400 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("tfc: download state for %s: HTTP %d", l.workspaceName, resp.StatusCode)
	}
	return resp.Body, nil
}

func (s *Source) doAuthed(ctx context.Context, method, url string) (*http.Response, error) {
	s.debugf("tfc: %s %s", method, url)
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.Token)
	req.Header.Set("Content-Type", "application/vnd.api+json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tfc: request %s: %w", url, err)
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("tfc: %s HTTP %d: %s", url, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return resp, nil
}
