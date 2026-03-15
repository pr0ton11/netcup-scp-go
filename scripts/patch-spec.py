#!/usr/bin/env python3
"""Patch the netcup SCP OpenAPI spec for oapi-codegen compatibility."""

import json
import sys

spec_file = sys.argv[1] if len(sys.argv) > 1 else "api/openapi.json"

with open(spec_file) as f:
    spec = json.load(f)

# 1. Strip application/hal+json from all response content types
#    The spec returns both application/json and application/hal+json for almost every endpoint.
#    This doubles the generated response type surface. We keep only application/json.
for path_obj in spec.get("paths", {}).values():
    for op in path_obj.values():
        if not isinstance(op, dict) or "responses" not in op:
            continue
        for resp in op["responses"].values():
            content = resp.get("content", {})
            if "application/hal+json" in content:
                del content["application/hal+json"]

# 2. Replace application/merge-patch+json with application/json in request bodies
#    oapi-codegen does not recognize application/merge-patch+json natively.
for path_obj in spec.get("paths", {}).values():
    for op in path_obj.values():
        if not isinstance(op, dict) or "requestBody" not in op:
            continue
        content = op["requestBody"].get("content", {})
        if "application/merge-patch+json" in content:
            content["application/json"] = content.pop("application/merge-patch+json")

# 3. Rewrite colon-style action paths (e.g. :format -> /format)
#    The spec uses Google's custom method convention (:action suffix) which is
#    non-standard for OpenAPI and causes issues with oapi-codegen's path parsing.
new_paths = {}
for path_key, path_obj in spec.get("paths", {}).items():
    new_key = (
        path_key.replace(":format", "/format")
        .replace(":reapply", "/reapply")
        .replace(":restore-copied-policies", "/restore-copied-policies")
        .replace(":dryrun", "/dryrun")
        .replace(":cancel", "/cancel")
    )
    new_paths[new_key] = path_obj
spec["paths"] = new_paths

with open(spec_file, "w") as f:
    json.dump(spec, f, indent=2)

print("Spec patched successfully")
