# drifthound

Detects AWS resources that aren't tracked by your Terraform code.

## Why

Most drift tools take a state file and report which **fields** of a resource changed since the last apply. drifthound answers a different question: **what's in your AWS account that no Terraform state file owns?**

It's built to handle the realistic case where infrastructure is split across many state files (per-environment, per-layer, per-team, Terragrunt module trees) and uses each state's scope — the accounts and regions it claims — to separate real drift from resources that simply aren't in scope for any state.

## Concepts

Every cloud resource lands in one of three buckets:

| Bucket | Meaning | Action |
| --- | --- | --- |
| `drift` | Exists in the provider, no state owns it, but some state's scope covers the (account, region) | Investigate — likely created out-of-band |
| `unscoped` | Exists in provider but **no** state file claims responsibility for that (account, region) | Add a state for this scope, or accept the gap |
| `ghost` | Listed in a state file but missing from the provider | Resource was deleted out-of-band; state needs refresh |

This three-bucket model is the main reason for drifthound's existence — graphing inventory tools don't make the scope-vs-drift distinction.

## Status

Alpha. End-to-end working: local-glob, S3, and Terraform Cloud / Enterprise state sources, Terraform state parsing, AWS Resource Explorer cloud collector, scope-aware diff. Terragrunt is not implemented yet.

## Install

From source:

```bash
go build -o drifthound ./cmd/drifthound
```

Requires Go 1.24 or newer.

Pre-built binaries are attached to each [GitHub release](../../releases) once a tag is published.

## Usage

Single state file:

```bash
./drifthound scan \
  --tfstate ./terraform.tfstate \
  --regions us-east-1,eu-west-1 \
  --use-explorer
```

Multiple state files via glob (note the quotes — let drifthound expand the glob, not the shell):

```bash
./drifthound scan \
  --tfstate "./envs/*/terraform.tfstate" \
  --regions us-east-1,eu-west-1 \
  --use-explorer
```

State files in S3 (one or more `s3://bucket/prefix` URIs; the prefix is walked recursively and every `.tfstate` / `.tfstate.json` / `.json` object is downloaded):

```bash
./drifthound scan \
  --tfstate-s3 s3://acme-tf-states/prod/ \
  --tfstate-s3 s3://acme-tf-states/staging/ \
  --regions us-east-1,eu-west-1 \
  --use-explorer
```

Terraform Cloud / Enterprise workspaces. By default every workspace in the org is included; use `--tfc-workspace` (exact name) and/or `--tfc-workspace-prefix` (matches everything starting with that string) to filter — the two are combined as a union:

```bash
export TFC_TOKEN=...   # or pass --tfc-token, or use TFE_TOKEN
./drifthound scan \
  --tfc-org acme \
  --tfc-workspace-prefix prod- \
  --tfc-workspace shared-services \
  --regions us-east-1,eu-west-1 \
  --use-explorer
```

For self-hosted Terraform Enterprise, add `--tfc-host tfe.acme.com`.

JSON output for CI checks:

```bash
./drifthound scan --tfstate ./terraform.tfstate --use-explorer --json \
  | jq '.drift | length'
```

Run `./drifthound scan --help` for the full flag list.

## Filtering noisy services

`--exclude-service` drops resources by AWS service identifier from **both sides** of the diff — without this the excluded cloud resources would show up as "unscoped" and the excluded code resources as "ghost". The filter is pushed into the Resource Explorer query (`-service:<name>`) so excluded resources aren't even fetched, and it also runs as a post-load pass to keep both sides symmetric.

```bash
./drifthound scan \
  --tfstate ./terraform.tfstate \
  --use-explorer \
  --exclude-service iam \
  --exclude-service eks
```

`resource-explorer-2` is always excluded — Resource Explorer's own indexes and views can't live in Terraform code, so leaving them in would always produce false "unscoped" entries.

## Debugging

`--debug` prints every outgoing URL / S3 URI to stderr. Useful when a TFC scan returns 401, an S3 prefix returns nothing, or you want to confirm what the Resource Explorer query looks like:

```bash
./drifthound scan --tfc-org acme --tfc-workspace-prefix prod- --debug
# stderr:
#   tfc: GET https://app.terraform.io/api/v2/organizations/acme/workspaces?page[number]=1&...
#   tfc: GET https://archivist.terraform.io/v1/object/...
```

Errors are printed without cobra's usage dump — only the error itself goes to stderr.

## Resource Explorer prerequisites

`--use-explorer` requires AWS Resource Explorer to be enabled in the target account:

1. Create a Resource Explorer index in each region you want covered.
2. Promote one of them to a **multi-region aggregator** index.
3. Set a **default view** in the aggregator region — or pass `--explorer-view <arn>` to override.

drifthound auto-discovers the aggregator region via `ListIndexes`, so the `--aws-region` flag only controls the bootstrap call. The actual `Search` runs in the aggregator region regardless of where you start.

## Required IAM permissions

```
resource-explorer-2:ListIndexes
resource-explorer-2:GetDefaultView
resource-explorer-2:Search
```

For `--tfstate-s3`, also `s3:ListBucket` and `s3:GetObject` on the relevant buckets.

## Development

```bash
go test ./...
go build ./...
```

