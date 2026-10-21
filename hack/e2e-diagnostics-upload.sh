#!/usr/bin/env bash

# Ships an e2e diagnostics directory (see hack/e2e-diagnostics.sh) to the
# project's S3-compatible object storage, Linode Object Storage, so the logs of
# every CI lane can be indexed and searched later. Meant for GitHub Actions
# only: the workflow passes the bucket credentials, and without them the script
# exits quietly (pull requests from forks never receive repository secrets).
#
# usage: ./hack/e2e-diagnostics-upload.sh [diagnostics dir]
#
# environment:
#   AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY  Linode Object Storage access key
#   E2E_LOGS_BUCKET, E2E_LOGS_ENDPOINT        bucket name and cluster URL
#                                             (https://eu-central-1.linodeobjects.com)
#   E2E_LANE                                  <distribution>-<version>-<phase>, e.g.
#                                             kind-v1.34.0-parallel, microshift-4.21-config
#   E2E_OUTCOME, E2E_HEAD_SHA, E2E_PR         optional labels taken from the workflow
#
# objects land under e2e/<date>/<run id>/<attempt>/<lane>/: the diagnostics files,
# gzipped, next to a metadata.json with the labels of the run.

set -euo pipefail

DIR="${1:-.e2e-diagnostics}"

if [ -z "${AWS_ACCESS_KEY_ID:-}" ] || [ -z "${AWS_SECRET_ACCESS_KEY:-}" ]; then
  echo "::notice::object storage credentials are not available (pull request from a fork?), not shipping $DIR"
  exit 0
fi

: "${E2E_LOGS_BUCKET:?set the LINODE_OBJ_BUCKET repository variable}"
: "${E2E_LOGS_ENDPOINT:?set the LINODE_OBJ_ENDPOINT repository variable}"
: "${E2E_LANE:?E2E_LANE must name the lane, e.g. kind-v1.34.0-parallel}"

if [ ! -d "$DIR" ]; then
  echo "::warning::$DIR does not exist, nothing to ship"
  exit 0
fi

# Linode signs requests with the cluster id that prefixes the endpoint host
# (https://eu-central-1.linodeobjects.com -> eu-central-1).
host="${E2E_LOGS_ENDPOINT#*://}"
host="${host%%/*}"
export AWS_DEFAULT_REGION="${AWS_DEFAULT_REGION:-${host%%.*}}"
# Recent AWS CLIs add checksum trailers to every upload, which S3-compatible
# stores reject; only compute them when the operation requires it.
export AWS_REQUEST_CHECKSUM_CALCULATION=when_required
export AWS_RESPONSE_CHECKSUM_VALIDATION=when_required

run_id="${GITHUB_RUN_ID:-local}"
attempt="${GITHUB_RUN_ATTEMPT:-1}"
key="e2e/$(date -u +%F)/$run_id/$attempt/$E2E_LANE"

stage="$(mktemp -d)"
trap 'rm -rf "$stage"' EXIT
cp -R "$DIR"/. "$stage"/
# Text files shrink about tenfold; the node tarballs are compressed already.
find "$stage" -type f \( -name '*.log' -o -name '*.txt' \) -exec gzip -6 {} +

pr="${E2E_PR:-}"
if [ -z "$pr" ] && [[ "${GITHUB_REF:-}" =~ ^refs/pull/([0-9]+)/ ]]; then
  pr="${BASH_REMATCH[1]}"
fi

# E2E_LANE is <distribution>-<version>-<phase>.
distribution="${E2E_LANE%%-*}"
phase="${E2E_LANE##*-}"
version="${E2E_LANE#*-}"
version="${version%-*}"

# Flat string labels for whoever indexes the bucket.
jq -n \
  --arg repository "${GITHUB_REPOSITORY:-}" \
  --arg sha "${E2E_HEAD_SHA:-${GITHUB_SHA:-}}" \
  --arg ref "${GITHUB_HEAD_REF:-${GITHUB_REF_NAME:-}}" \
  --arg pull_request "$pr" \
  --arg event "${GITHUB_EVENT_NAME:-}" \
  --arg run_id "$run_id" \
  --arg run_attempt "$attempt" \
  --arg run_url "${GITHUB_SERVER_URL:-https://github.com}/${GITHUB_REPOSITORY:-}/actions/runs/$run_id/attempts/$attempt" \
  --arg job "${GITHUB_JOB:-}" \
  --arg lane "$E2E_LANE" \
  --arg distribution "$distribution" \
  --arg kubernetes_version "$version" \
  --arg phase "$phase" \
  --arg outcome "${E2E_OUTCOME:-}" \
  --arg collected_at "$(cat "$DIR/collected-at.txt" 2>/dev/null || true)" \
  --arg uploaded_at "$(date -u +%FT%TZ)" \
  '$ARGS.named' > "$stage/metadata.json"

aws s3 cp "$stage" "s3://$E2E_LOGS_BUCKET/$key/" --recursive --no-progress --endpoint-url "$E2E_LOGS_ENDPOINT"
echo "shipped $(find "$stage" -type f | wc -l | tr -d ' ') files to s3://$E2E_LOGS_BUCKET/$key/"
