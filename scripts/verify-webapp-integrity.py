# /// script
# dependencies = [
#   "requests",
# ]
# ///

# This script verifies a remote deployment of the Privatemode web app
# against a locally-built version to check its integrity.
# Specifically, it verifies:
# - That all HTML files in the local reference directory are present remotely
#   and match the local content byte-by-byte.
# - That each HTML file contains an importmap with integrity hashes for
#   *all* JavaScript modules in the build output.

import argparse
import glob
import json
import os
import re
import sys
import requests

IMPORTMAP_RE = re.compile(r'<script type="importmap">\s*(.*?)\s*</script>', re.DOTALL)


def collect_js_assets(reference_dir: str) -> set[str]:
    immutable_dir = os.path.join(reference_dir, "_app", "immutable")
    js_files = glob.glob(os.path.join(immutable_dir, "**", "*.js"), recursive=True)
    assets = set()
    for path in js_files:
        rel = os.path.relpath(path, reference_dir).replace(os.sep, "/")
        assets.add(f"/{rel}")
    return assets


def parse_importmap(html: str) -> dict[str, str] | None:
    m = IMPORTMAP_RE.search(html)
    if not m:
        return None
    try:
        data = json.loads(m.group(1))
    except json.JSONDecodeError:
        return None
    return data.get("integrity")


# For a given HTML file path relative to the reference directory, return the
# list of remote URLs that should serve the same content. For example:
# - "index.html" -> ["/", "/index.html"]
# - "security.html" -> ["/security", "/security.html"]
def route_urls(rel: str) -> list[str]:
    urls = [f"/{rel}"]
    stem, _ = os.path.splitext(rel)
    if rel == "index.html":
        urls.append("/")
    elif not rel.endswith("/index.html"):
        urls.append(f"/{stem}")
    else:
        # e.g. "subdir/index.html" -> "/subdir/"
        urls.append(f"/{stem.rsplit('/index', 1)[0]}/")
    return urls


def main():
    parser = argparse.ArgumentParser(description="Verify webapp integrity")
    parser.add_argument(
        "--reference-dir",
        required=True,
        help="Local directory with reference HTML files",
    )
    parser.add_argument("--url", required=True, help="Remote base URL to check against")
    args = parser.parse_args()

    html_files = glob.glob(
        os.path.join(args.reference_dir, "**", "*.html"), recursive=True
    )
    if not html_files:
        print(f"No HTML files found in {args.reference_dir}")
        sys.exit(1)

    js_assets = collect_js_assets(args.reference_dir)
    print(f"Found {len(js_assets)} JS assets in reference directory")

    base_url = args.url.rstrip("/")
    errors = []

    for path in sorted(html_files):
        rel = os.path.relpath(path, args.reference_dir)
        print(f"Checking {rel} ...")

        with open(path) as f:
            local_content = f.read()

        integrity = parse_importmap(local_content)
        if integrity is None:
            errors.append(f"{rel}: missing or invalid importmap")
            continue

        missing = js_assets - integrity.keys()
        if missing:
            for asset in sorted(missing):
                errors.append(f"{rel}: JS asset {asset} missing from importmap")
            continue

        for route in route_urls(rel):
            url = f"{base_url}{route}"
            print(f"  {route} -> {url}")
            resp = requests.get(url)
            if resp.status_code != 200:
                errors.append(f"{rel}: {route}: remote returned {resp.status_code}")
                continue
            if resp.text != local_content:
                errors.append(
                    f"{rel}: {route}: remote content does not match local reference"
                )

    if errors:
        print("\nErrors:")
        for e in errors:
            print(f"  - {e}")
        sys.exit(1)

    print(f"\nAll {len(html_files)} HTML files verified successfully.")


if __name__ == "__main__":
    main()
