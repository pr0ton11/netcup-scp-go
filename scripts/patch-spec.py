#!/usr/bin/env python3
"""Patch the netcup SCP OpenAPI spec for oapi-codegen compatibility."""

import json
import re
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

# 3. Name colon-style action paths (e.g. /firewall:reapply)
#    The spec uses Google's custom method convention (:action suffix) and has no operationIds.
#    The path must stay as it is, since it is the URL the server serves. We only set an
#    operationId so the generated method names stay stable, e.g.
#    PostApiV1ServersServerIdInterfacesMacFirewallReapply.
def operation_id(method, path):
    words = [method] + re.split(r"[/:{}\-]+", path)
    return "".join(w[:1].upper() + w[1:] for w in words if w)


for path_key, path_obj in spec.get("paths", {}).items():
    if ":" not in path_key:
        continue
    for method, op in path_obj.items():
        if isinstance(op, dict) and "operationId" not in op:
            op["operationId"] = operation_id(method, path_key)

# 4. Downgrade OpenAPI 3.1 schemas to 3.0
#    oapi-codegen does not support OpenAPI 3.1 (https://github.com/oapi-codegen/oapi-codegen/issues/373).
#    Upstream switched to 3.1 in October 2026. The spec only uses a few 3.1 features:
#    - "type": ["string", "null"]            -> "type": "string", "nullable": true
#    - "anyOf": [{"$ref": ...}, {"type": "null"}] -> "$ref": ... (optional fields are pointers anyway)
#    - "examples": [...] inside a schema      -> "example": first item
#    Anything else 3.1-specific fails the script, so a new feature is noticed instead of
#    being generated into a wrong client.
SCHEMA_31_KEYWORDS = {
    "const", "prefixItems", "contentMediaType", "contentEncoding", "unevaluatedProperties",
    "unevaluatedItems", "if", "then", "else", "dependentRequired", "dependentSchemas",
    "patternProperties", "$defs",
}


def is_null_schema(s):
    return isinstance(s, dict) and s.get("type") == "null" and len(s) == 1


def downgrade_schema(s, where):
    if isinstance(s, list):
        for i, item in enumerate(s):
            downgrade_schema(item, f"{where}[{i}]")
        return
    if not isinstance(s, dict):
        return

    t = s.get("type")
    if isinstance(t, list):
        types = [x for x in t if x != "null"]
        if len(types) != 1:
            sys.exit(f"unsupported type list {t} at {where}")
        s["type"] = types[0]
        if "null" in t:
            s["nullable"] = True

    for key in ("anyOf", "oneOf"):
        variants = s.get(key)
        if isinstance(variants, list) and any(is_null_schema(v) for v in variants):
            rest = [v for v in variants if not is_null_schema(v)]
            if len(rest) != 1:
                sys.exit(f"unsupported nullable {key} at {where}")
            del s[key]
            if "$ref" in rest[0] and len(rest[0]) == 1 and not s:
                s["$ref"] = rest[0]["$ref"]
            else:
                s.update(rest[0])
                s["nullable"] = True

    if isinstance(s.get("examples"), list):
        examples = s.pop("examples")
        if examples and "example" not in s:
            s["example"] = examples[0]

    if isinstance(s.get("exclusiveMinimum"), (int, float)) and not isinstance(s["exclusiveMinimum"], bool):
        s["minimum"], s["exclusiveMinimum"] = s["exclusiveMinimum"], True
    if isinstance(s.get("exclusiveMaximum"), (int, float)) and not isinstance(s["exclusiveMaximum"], bool):
        s["maximum"], s["exclusiveMaximum"] = s["exclusiveMaximum"], True

    found = SCHEMA_31_KEYWORDS & s.keys()
    if found:
        sys.exit(f"unsupported OpenAPI 3.1 keywords {sorted(found)} at {where}")

    for key, value in s.items():
        if key == "properties":
            for name, prop in value.items():
                downgrade_schema(prop, f"{where}.properties.{name}")
        elif key in ("items", "additionalProperties", "not"):
            downgrade_schema(value, f"{where}.{key}")
        elif key in ("allOf", "anyOf", "oneOf"):
            downgrade_schema(value, f"{where}.{key}")


def downgrade_content(content, where):
    for media_type, media in content.items():
        if "schema" in media:
            downgrade_schema(media["schema"], f"{where}.{media_type}.schema")


if spec.get("openapi", "").startswith("3.1"):
    spec["openapi"] = "3.0.3"
    spec.pop("jsonSchemaDialect", None)
    spec.pop("webhooks", None)

    components = spec.get("components", {})
    for name, schema in components.get("schemas", {}).items():
        downgrade_schema(schema, f"components.schemas.{name}")
    for name, resp in components.get("responses", {}).items():
        downgrade_content(resp.get("content", {}), f"components.responses.{name}")
    for name, param in components.get("parameters", {}).items():
        downgrade_schema(param.get("schema"), f"components.parameters.{name}")
    for name, body in components.get("requestBodies", {}).items():
        downgrade_content(body.get("content", {}), f"components.requestBodies.{name}")

    for path_key, path_obj in spec.get("paths", {}).items():
        for param in path_obj.get("parameters", []):
            downgrade_schema(param.get("schema"), f"{path_key}.parameters")
        for method, op in path_obj.items():
            if not isinstance(op, dict):
                continue
            where = f"{method.upper()} {path_key}"
            for param in op.get("parameters", []):
                downgrade_schema(param.get("schema"), f"{where}.parameters.{param.get('name')}")
            if "requestBody" in op:
                downgrade_content(op["requestBody"].get("content", {}), f"{where}.requestBody")
            for code, resp in op.get("responses", {}).items():
                downgrade_content(resp.get("content", {}), f"{where}.responses.{code}")

with open(spec_file, "w") as f:
    json.dump(spec, f, indent=2)
    f.write("\n")

print("Spec patched successfully")
