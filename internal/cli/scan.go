package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"drifthound/internal/awsclient"
	"drifthound/internal/collector/cloud/explorer"
	"drifthound/internal/diff"
	"drifthound/internal/resource"
	"drifthound/internal/state"
	"drifthound/internal/state/local"
	"drifthound/internal/state/s3source"
	"drifthound/internal/state/tfcsource"
	"drifthound/internal/state/tfstate"
)

type sourceFlags struct {
	globs     []string
	s3URIs    []string
	awsRegion string

	tfcOrg        string
	tfcToken      string
	tfcHost       string
	tfcWorkspaces []string
	tfcPrefixes   []string
}

func newScanCommand() *cobra.Command {
	var (
		src             sourceFlags
		regions         []string
		accounts        []string
		excludeServices []string
		jsonOut         bool
		useExplorer     bool
		explorerView    string
		debug           bool
	)

	cmd := &cobra.Command{
		Use:          "scan",
		Short:        "Scan AWS and diff against Terraform state",
		SilenceUsage: true,
		RunE: func(c *cobra.Command, _ []string) error {
			ctx := c.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			var debugLogger func(format string, args ...any)
			if debug {
				debugLogger = func(format string, args ...any) {
					fmt.Fprintf(os.Stderr, format+"\n", args...)
				}
			}

			sources, err := buildStateSources(ctx, src, debugLogger)
			if err != nil {
				return err
			}

			states, err := loadStates(ctx, sources, accounts, regions)
			if err != nil {
				return err
			}

			// Resource Explorer's own resources (indexes, views) can never
			// live in Terraform code — exclude them unconditionally so they
			// don't pollute the unscoped bucket whenever --use-explorer is on.
			excludeServices = append(excludeServices, "resource-explorer-2")

			var cloudRes []resource.Resource
			if useExplorer {
				cloudRes, err = collectExplorer(ctx, src.awsRegion, explorerView, regions, excludeServices)
				if err != nil {
					return err
				}
			}

			if len(excludeServices) > 0 {
				excludeSet := make(map[string]struct{}, len(excludeServices))
				for _, s := range excludeServices {
					excludeSet[strings.ToLower(s)] = struct{}{}
				}
				cloudRes = filterOutServices(cloudRes, excludeSet)
				for i := range states {
					states[i].Resources = filterOutServices(states[i].Resources, excludeSet)
				}
			}

			result := diff.Compute(cloudRes, states)

			if jsonOut {
				return json.NewEncoder(os.Stdout).Encode(result)
			}
			printTable(result, len(states))
			return nil
		},
	}

	cmd.Flags().StringSliceVar(&src.globs, "tfstate", nil, "glob(s) of local terraform state JSON files")
	cmd.Flags().StringSliceVar(&src.s3URIs, "tfstate-s3", nil, "s3://bucket/prefix URI(s) — every state-shaped object under each prefix is downloaded and parsed")
	cmd.Flags().StringVar(&src.tfcOrg, "tfc-org", "", "Terraform Cloud / Enterprise organization (enables the TFC source)")
	cmd.Flags().StringVar(&src.tfcToken, "tfc-token", "", "TFC/TFE API token (falls back to TFC_TOKEN or TFE_TOKEN env)")
	cmd.Flags().StringVar(&src.tfcHost, "tfc-host", "", "TFC/TFE host (default app.terraform.io)")
	cmd.Flags().StringSliceVar(&src.tfcWorkspaces, "tfc-workspace", nil, "exact workspace name to include (repeatable)")
	cmd.Flags().StringSliceVar(&src.tfcPrefixes, "tfc-workspace-prefix", nil, "workspace name prefix to include (repeatable)")
	cmd.Flags().StringVar(&src.awsRegion, "aws-region", "", "bootstrap region for the AWS client (defaults to AWS_REGION / shared config)")
	cmd.Flags().StringSliceVar(&regions, "regions", nil, "AWS regions in scope")
	cmd.Flags().StringSliceVar(&accounts, "accounts", nil, "AWS account IDs in scope")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit JSON instead of a table")
	cmd.Flags().BoolVar(&useExplorer, "use-explorer", false, "enumerate live resources via AWS Resource Explorer")
	cmd.Flags().StringVar(&explorerView, "explorer-view", "", "Resource Explorer view ARN (auto-detected from the aggregator index if empty)")
	cmd.Flags().BoolVar(&debug, "debug", false, "log every outgoing URL / S3 URI to stderr")
	cmd.Flags().StringSliceVar(&excludeServices, "exclude-service", nil, "AWS service identifiers to drop from the diff on both sides (e.g. iam, eks). 'resource-explorer-2' is always excluded. Repeatable.")

	return cmd
}

// buildStateSources constructs every configured state source. AWS config is
// only loaded if at least one S3 URI is supplied, so pure-local mode still
// works without AWS creds. debugLogger, when non-nil, is attached to every
// source so each one logs the URLs / URIs of its outgoing requests.
func buildStateSources(ctx context.Context, src sourceFlags, debugLogger func(format string, args ...any)) ([]state.Source, error) {
	var sources []state.Source

	if len(src.globs) > 0 {
		sources = append(sources, &local.Source{Patterns: src.globs})
	}

	if len(src.s3URIs) > 0 {
		cfg, err := awsclient.LoadDefault(ctx, src.awsRegion)
		if err != nil {
			return nil, err
		}
		for _, uri := range src.s3URIs {
			bucket, prefix, err := s3source.ParseURI(uri)
			if err != nil {
				return nil, err
			}
			s3src := s3source.New(cfg, bucket, prefix)
			s3src.Debugf = debugLogger
			sources = append(sources, s3src)
		}
	}

	if src.tfcOrg != "" {
		token := src.tfcToken
		if token == "" {
			token = os.Getenv("TFC_TOKEN")
		}
		if token == "" {
			token = os.Getenv("TFE_TOKEN")
		}
		tfc, err := tfcsource.New(tfcsource.Config{
			Host:         src.tfcHost,
			Token:        token,
			Organization: src.tfcOrg,
			Workspaces:   src.tfcWorkspaces,
			Prefixes:     src.tfcPrefixes,
			Logf: func(format string, args ...any) {
				fmt.Fprintf(os.Stderr, format+"\n", args...)
			},
			Debugf: debugLogger,
		})
		if err != nil {
			return nil, err
		}
		sources = append(sources, tfc)
	}

	return sources, nil
}

func collectExplorer(ctx context.Context, region, viewARN string, regionFilter, excludeServices []string) ([]resource.Resource, error) {
	cfg, err := awsclient.LoadDefault(ctx, region)
	if err != nil {
		return nil, err
	}
	coll := explorer.New()
	coll.ViewARN = viewARN
	coll.Regions = regionFilter
	coll.ExcludeServices = excludeServices

	resCh, errCh := coll.Enumerate(ctx, cfg)
	var out []resource.Resource
	for r := range resCh {
		out = append(out, r)
	}
	for err := range errCh {
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func loadStates(ctx context.Context, sources []state.Source, accounts, regions []string) ([]diff.ScopedState, error) {
	var out []diff.ScopedState
	for _, src := range sources {
		refs, err := src.Discover(ctx)
		if err != nil {
			return nil, err
		}
		for _, ref := range refs {
			rc, err := ref.Loader.Load(ctx)
			if err != nil {
				return nil, fmt.Errorf("load %s: %w", ref.Name, err)
			}
			resources, err := tfstate.Parse(rc, ref.Name)
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("parse %s: %w", ref.Name, err)
			}

			sc := ref.Scope
			if len(accounts) > 0 {
				sc.Accounts = accounts
			}
			if len(regions) > 0 {
				sc.Regions = regions
			}
			if len(sc.Accounts) == 0 || len(sc.Regions) == 0 {
				inferred := inferScope(resources)
				if len(sc.Accounts) == 0 {
					sc.Accounts = inferred.Accounts
				}
				if len(sc.Regions) == 0 {
					sc.Regions = inferred.Regions
				}
			}

			out = append(out, diff.ScopedState{
				Name:      ref.Name,
				Scope:     sc,
				Resources: resources,
			})
		}
	}
	return out, nil
}

func inferScope(rs []resource.Resource) state.Scope {
	accSet := map[string]struct{}{}
	regSet := map[string]struct{}{}
	for _, r := range rs {
		if r.Account != "" {
			accSet[r.Account] = struct{}{}
		}
		if r.Region != "" {
			regSet[r.Region] = struct{}{}
		}
	}
	return state.Scope{
		Accounts: keys(accSet),
		Regions:  keys(regSet),
	}
}

func filterOutServices(rs []resource.Resource, exclude map[string]struct{}) []resource.Resource {
	out := make([]resource.Resource, 0, len(rs))
	for _, r := range rs {
		if _, blocked := exclude[strings.ToLower(r.Service)]; blocked {
			continue
		}
		out = append(out, r)
	}
	return out
}

func keys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func printTable(r diff.Result, statesLoaded int) {
	fmt.Printf("states:   %d\n", statesLoaded)
	fmt.Printf("drift:    %d\n", len(r.Drift))
	fmt.Printf("ghost:    %d\n", len(r.Ghost))
	fmt.Printf("unscoped: %d\n", len(r.Unscoped))
	if len(r.Drift) > 0 {
		fmt.Println("\nDRIFT (in cloud, not in code):")
		for _, x := range r.Drift {
			fmt.Printf("  %s\t%s\t%s\n", x.Account, x.Region, x.ARN)
		}
	}
	if len(r.Unscoped) > 0 {
		fmt.Println("\nUNSCOPED (in cloud, outside every state's scope):")
		for _, x := range r.Unscoped {
			fmt.Printf("  %s\t%s\t%s\n", x.Account, x.Region, x.ARN)
		}
	}
	if len(r.Ghost) > 0 {
		fmt.Println("\nGHOST (in code, not in cloud):")
		for _, x := range r.Ghost {
			fmt.Printf("  %s\t%s\t%s\n", x.Account, x.Region, x.ARN)
		}
	}
}
