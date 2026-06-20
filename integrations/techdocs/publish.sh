#!/usr/bin/env bash
#
# Build and publish the ADP TechDocs sites produced by `adp-techdocs`.
#
# Prereqs:
#   - techdocs-cli installed:  npm install -g @techdocs/cli
#   - a MkDocs generator: either local mkdocs + the techdocs-core plugin
#     (use GEN_FLAGS="--no-docker", the default) or Docker (set GEN_FLAGS="")
#   - cloud credentials for your publisher (e.g. AWS creds for awsS3)
#
# Usage:
#   SRC_DIR=./techdocs-out PUBLISHER=awsS3 BUCKET=my-techdocs-bucket ./publish.sh
#
set -euo pipefail

SRC_DIR="${SRC_DIR:-./techdocs-out}"
PUBLISHER="${PUBLISHER:-awsS3}"
BUCKET="${BUCKET:?set BUCKET to your TechDocs storage bucket/container}"
GEN_FLAGS="${GEN_FLAGS:---no-docker}"

found=0
while IFS= read -r -d '' mkdocs; do
  found=1
  dir="$(dirname "$mkdocs")"
  # Entity triplet = the three path segments under SRC_DIR: namespace/kind/name.
  # techdocs-cli --entity is case-sensitive and must match how your catalog
  # references the entity (re-run adp-techdocs with -kind component if your
  # TechDocs serves lowercased refs).
  entity="${dir#"${SRC_DIR%/}/"}"
  echo ">> ${entity}"
  techdocs-cli generate ${GEN_FLAGS} --source-dir "$dir" --output-dir "$dir/site"
  techdocs-cli publish \
    --publisher-type "$PUBLISHER" \
    --storage-name "$BUCKET" \
    --entity "$entity" \
    --directory "$dir/site"
done < <(find "$SRC_DIR" -name mkdocs.yml -print0)

if [[ "$found" -eq 0 ]]; then
  echo "no MkDocs sites found under ${SRC_DIR}; run adp-techdocs first" >&2
  exit 1
fi
