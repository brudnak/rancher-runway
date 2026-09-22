#!/usr/bin/env python3
"""Release published source using GitHub APIs; never commit application files."""

import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import time
from urllib.parse import quote


ROOT = Path(__file__).resolve().parent.parent
SOURCE_REPO = "brudnak/rancher-runway"
TAP_REPO = "brudnak/homebrew-tap"
CASK_PATH = "Casks/rancher-runway.rb"
WORKFLOW = "release-macos.yml"
VERSION_RE = re.compile(r"v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\Z")
RELEASE_FILES = (
    "Makefile", "README.md", ".github/workflows/release-macos.yml",
    "scripts/release.py", "scripts/package-macos-release.sh",
    "scripts/test_release.py", "scripts/macos_release_test.go",
    "scripts/render-homebrew-cask.sh", "packaging/homebrew/Casks/rancher-runway.rb.tmpl",
    "docs/homebrew-release.md",
)


class ReleaseError(Exception):
    pass


def gh(*args, body=None):
    result = subprocess.run(
        ["gh", *args], input=body, text=True, capture_output=True, check=False,
    )
    if result.returncode:
        raise ReleaseError(result.stderr.strip() or result.stdout.strip() or "GitHub CLI failed")
    return result.stdout


def api(endpoint, method="GET", payload=None, missing_ok=False):
    args = ["gh", "api", "--hostname", "github.com", "--method", method, endpoint]
    if payload is not None:
        args += ["--input", "-"]
    result = subprocess.run(args, input=json.dumps(payload) if payload is not None else None,
                            text=True, capture_output=True, check=False)
    try:
        value = json.loads(result.stdout) if result.stdout.strip() else None
    except json.JSONDecodeError as exc:
        raise ReleaseError(result.stderr.strip() or "GitHub returned invalid JSON") from exc
    if result.returncode:
        if missing_ok and isinstance(value, dict) and str(value.get("status")) == "404":
            return None
        raise ReleaseError(result.stderr.strip() or str(value))
    return value


def all_pages(endpoint):
    items = []
    for page in range(1, 1001):
        batch = api(f"{endpoint}?per_page=100&page={page}")
        items.extend(batch)
        if len(batch) < 100:
            return items
    raise ReleaseError("Too many GitHub pages; refusing to choose a version from incomplete data")


def parse_version(value):
    match = VERSION_RE.fullmatch(value)
    if not match:
        raise ReleaseError(f"Expected a stable MAJOR.MINOR.PATCH version, got {value!r}")
    return tuple(map(int, match.groups()))


def next_version(tags, bump):
    versions = [parse_version(tag) for tag in tags if VERSION_RE.fullmatch(tag)]
    if not versions:
        return "v1.0.0"
    major, minor, patch = max(versions)
    if bump == "major":
        major, minor, patch = major + 1, 0, 0
    elif bump == "minor":
        minor, patch = minor + 1, 0
    else:
        patch += 1
    return f"v{major}.{minor}.{patch}"


def source_mismatches(tree, root=ROOT):
    if tree.get("truncated"):
        raise ReleaseError("GitHub source tree is incomplete; cannot verify release source")
    mismatches = set()
    paths = set()
    for entry in tree["tree"]:
        if entry["type"] != "blob":
            continue
        relative = Path(entry["path"])
        if relative.is_absolute() or ".." in relative.parts:
            raise ReleaseError("Unexpected source tree path")
        paths.add(entry["path"])
        path = root / relative
        try:
            content = os.fsencode(os.readlink(path)) if entry["mode"] == "120000" else path.read_bytes()
        except (OSError, ValueError):
            mismatches.add(entry["path"])
            continue
        # Match GitHub's blob object ID without invoking Git.
        digest = hashlib.sha1(b"blob " + str(len(content)).encode() + b"\0" + content).hexdigest()
        if digest != entry["sha"]:
            mismatches.add(entry["path"])
    mismatches.update(set(RELEASE_FILES) - paths)
    return sorted(mismatches)


def find_run(tag, commit):
    data = api(f"repos/{SOURCE_REPO}/actions/workflows/{WORKFLOW}/runs?head_sha={commit}&per_page=100")
    matches = [run for run in data["workflow_runs"]
               if run["head_sha"] == commit and run.get("display_title") == f"Release Rancher Runway {tag}"]
    return max(matches, key=lambda run: run["id"]) if matches else None


def wait_for_run(tag, commit):
    deadline = time.monotonic() + 180
    while time.monotonic() < deadline:
        run = find_run(tag, commit)
        if run:
            break
        time.sleep(5)
    else:
        raise ReleaseError(f"No release run started for {tag}. Retry with RELEASE_VERSION={tag}; the tag was preserved.")
    print(f"Workflow: {run['html_url']}", flush=True)
    deadline = time.monotonic() + 100 * 60
    previous = None
    while time.monotonic() < deadline:
        status = run["status"]
        if status != previous:
            print(f"Release workflow: {status}", flush=True)
            previous = status
        if status == "completed":
            if run["conclusion"] != "success":
                raise ReleaseError(f"Release workflow {run['conclusion']}: {run['html_url']}. Retry with RELEASE_VERSION={tag} after fixing the failure.")
            return
        time.sleep(15)
        run = api(f"repos/{SOURCE_REPO}/actions/runs/{run['id']}")
    raise ReleaseError(f"Timed out waiting; the workflow may still be running: {run['html_url']}")


def validate_cask(text, version, checksum, url):
    expected = {"version": version, "sha256": checksum, "url": url, "app": "Rancher Runway.app"}
    if not text.startswith('cask "rancher-runway" do\n') or re.search(r"@[A-Z_]+@", text):
        raise ReleaseError("Release Cask has an unexpected name or unrendered placeholders")
    for key, value in expected.items():
        values = re.findall(rf'^[ \t]*{key}[ \t]+"([^"\n]+)"[ \t]*,?[ \t]*$', text, re.MULTILINE)
        if values != [value]:
            raise ReleaseError(f"Release Cask {key} does not match the downloaded release")


def update_tap(tag):
    version = tag[1:]
    dmg_name = f"Rancher-Runway-{version}-macOS-universal.dmg"
    with tempfile.TemporaryDirectory(prefix="runway-release-") as temp:
        gh("release", "download", tag, "--repo", f"https://github.com/{SOURCE_REPO}",
           "--pattern", "rancher-runway.rb", "--pattern", dmg_name, "--dir", temp)
        cask = (Path(temp) / "rancher-runway.rb").read_bytes()
        digest = hashlib.sha256()
        with (Path(temp) / dmg_name).open("rb") as dmg:
            for block in iter(lambda: dmg.read(1024 * 1024), b""):
                digest.update(block)
        validate_cask(cask.decode(), version, digest.hexdigest(),
                      f"https://github.com/{SOURCE_REPO}/releases/download/{tag}/{dmg_name}")
    endpoint = f"repos/{TAP_REPO}/contents/{CASK_PATH}"
    current = api(endpoint, missing_ok=True)
    payload = {"message": f"rancher-runway {version}", "content": base64.b64encode(cask).decode()}
    if current:
        previous = base64.b64decode(current["content"])
        if previous == cask:
            print("Homebrew tap already matches this release.", flush=True)
            return
        previous_versions = re.findall(rb'^[ \t]*version "([^"]+)"', previous, re.MULTILINE)
        if len(previous_versions) != 1:
            raise ReleaseError("Cannot determine the current tap version; refusing to replace it")
        if parse_version(previous_versions[0].decode()) > parse_version(version):
            raise ReleaseError("The Homebrew tap has a newer version; refusing to downgrade it")
        payload["sha"] = current["sha"]
    api(endpoint, method="PUT", payload=payload)
    print(f"Updated https://github.com/{TAP_REPO}/blob/main/{CASK_PATH}", flush=True)


def run(args):
    tags = all_pages(f"repos/{SOURCE_REPO}/tags")
    tag = "v" + ".".join(map(str, parse_version(args.version))) if args.version else next_version([item["name"] for item in tags], args.bump)
    print(f"Release version: {tag}", flush=True)
    release = api(f"repos/{SOURCE_REPO}/releases/tags/{tag}", missing_ok=True)
    if release:
        if release["draft"] or release["prerelease"]:
            raise ReleaseError(f"{tag} already exists as a draft or prerelease; resolve it before retrying")
        print(f"Release already published: {release['html_url']}", flush=True)
        if args.dry_run:
            print("Would verify release artifacts and update the Homebrew tap. No changes made.")
        else:
            update_tap(tag)
        return
    matching_tag = next((item for item in tags if item["name"] == tag), None)
    ref = tag if matching_tag else args.ref
    commit = api(f"repos/{SOURCE_REPO}/commits/{quote(ref, safe='')}")
    sha = commit["sha"]
    print(f"Source: {SOURCE_REPO}@{ref} ({sha})", flush=True)
    tree = api(f"repos/{SOURCE_REPO}/git/trees/{commit['commit']['tree']['sha']}?recursive=1")
    mismatches = source_mismatches(tree)
    if mismatches:
        listing = "\n".join(f"  {path}" for path in mismatches)
        raise ReleaseError("Local source and release files are not published at that ref:\n" + listing +
                           "\nPublish the intended source first, then rerun. No tag or release was created.")
    workflow = api(f"repos/{SOURCE_REPO}/actions/workflows/{WORKFLOW}")
    if workflow["state"] != "active":
        raise ReleaseError("The macOS release workflow is not active")
    api(f"repos/{TAP_REPO}")
    print("Will publish the app, DMG/ZIP, checksums, and Cask, then update the tap (a Cask commit).", flush=True)
    if args.dry_run:
        print("Plan only. No changes made.")
        return
    if matching_tag:
        existing_run = find_run(tag, sha)
        if not existing_run:
            gh("workflow", "run", WORKFLOW, "--repo", f"https://github.com/{SOURCE_REPO}", "--ref", tag, "-f", f"tag={tag}")
        elif existing_run["status"] == "completed":
            gh("run", "rerun", str(existing_run["id"]), "--repo", f"https://github.com/{SOURCE_REPO}")
            # Wait for the rerun to leave its previous completed state.
            for _ in range(36):
                if api(f"repos/{SOURCE_REPO}/actions/runs/{existing_run['id']}")["status"] != "completed":
                    break
                time.sleep(5)
    else:
        api(f"repos/{SOURCE_REPO}/git/refs", method="POST", payload={"ref": f"refs/tags/{tag}", "sha": sha})
        print(f"Created {tag}; its tag event starts the release workflow.", flush=True)
    wait_for_run(tag, sha)
    release = api(f"repos/{SOURCE_REPO}/releases/tags/{tag}")
    if release["draft"] or release["prerelease"]:
        raise ReleaseError("Workflow did not publish a stable release")
    print(f"Published: {release['html_url']}", flush=True)
    update_tap(tag)
    print("Install: brew install --cask brudnak/tap/rancher-runway", flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--dry-run", action="store_true", help="read-only release plan")
    parser.add_argument("--bump", choices=("patch", "minor", "major"), default=os.getenv("RELEASE_BUMP", "patch"))
    parser.add_argument("--version", default=os.getenv("RELEASE_VERSION", ""), help="explicit stable version, also used to retry a release")
    parser.add_argument("--ref", default=os.getenv("RELEASE_REF", "main"), help="published source branch or commit")
    args = parser.parse_args()
    if args.bump not in ("patch", "minor", "major"):
        parser.error("RELEASE_BUMP must be patch, minor, or major")
    try:
        run(args)
    except (ReleaseError, OSError, ValueError, KeyError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1
    except KeyboardInterrupt:
        print("Stopped waiting. Any GitHub workflow already started continues remotely; retry with its explicit RELEASE_VERSION.", file=sys.stderr)
        return 130
    return 0


if __name__ == "__main__":
    sys.exit(main())
