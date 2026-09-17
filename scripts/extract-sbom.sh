#!/usr/bin/env bash
#
# Extract the SPDX SBOM from a buildx OCI archive built with --sbom=true.
#
#   ./scripts/extract-sbom.sh dist/image-oci.tar dist/sbom.spdx.json
#
# BuildKit attaches the SBOM as an in-toto attestation inside the OCI
# index rather than as a loose file, which is the right place for it — it
# travels with the image and is verifiable. This script exists so a
# maintainer can read it locally without a registry or extra tooling.
#
# See docs/supply-chain.md.

set -euo pipefail

archive="${1:?usage: extract-sbom.sh <oci-archive.tar> <output.json>}"
output="${2:?usage: extract-sbom.sh <oci-archive.tar> <output.json>}"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

tar -xf "$archive" -C "$work"

python3 - "$work" "$output" <<'PYEOF'
import json, pathlib, sys

oci, out = pathlib.Path(sys.argv[1]), pathlib.Path(sys.argv[2])


def blob(digest):
    algo, hexd = digest.split(":")
    return oci / "blobs" / algo / hexd


index = json.loads((oci / "index.json").read_text())
top = json.loads(blob(index["manifests"][0]["digest"]).read_text())

for manifest in top["manifests"]:
    # Attestation manifests are the ones with an "unknown" platform; the
    # real per-platform images carry a real architecture.
    if manifest.get("platform", {}).get("architecture") != "unknown":
        continue
    for layer in json.loads(blob(manifest["digest"]).read_text())["layers"]:
        doc = json.loads(blob(layer["digest"]).read_text())
        predicate = doc.get("predicate", {})
        if "spdxVersion" not in predicate:
            continue
        out.write_text(json.dumps(predicate, indent=2))
        print(
            f"SPDX {predicate['spdxVersion']}, "
            f"{len(predicate.get('packages', []))} packages -> {out}"
        )
        raise SystemExit(0)

print("no SPDX attestation found in archive", file=sys.stderr)
raise SystemExit(1)
PYEOF
