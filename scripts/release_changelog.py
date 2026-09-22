#!/usr/bin/env python3
"""Roll the [Unreleased] section of CHANGELOG.md into a new dated version section.

Used by .github/workflows/release.yml. Can also be run locally to preview a release:

    python3 scripts/release_changelog.py \\
        --changelog CHANGELOG.md --version 1.1.0 --previous 1.0.0 \\
        --date 2026-10-01 --repo https://github.com/DanielGS/cloudflare-smtp-relay
"""

import argparse
import re
import sys


def rewrite(text: str, version: str, previous: str, date: str, repo: str) -> str:
    lines = text.split("\n")

    unreleased_idx = next(
        (i for i, line in enumerate(lines) if line.startswith("## [Unreleased]")),
        None,
    )
    if unreleased_idx is None:
        raise ValueError("could not find a '## [Unreleased]' heading")

    # Refuse to run twice for the same version. The release workflow already
    # guards against this, but this script is runnable by hand, and a second
    # pass would silently duplicate both the version heading and its link
    # reference instead of failing.
    if any(line.startswith(f"## [{version}]") for line in lines):
        raise ValueError(
            f"CHANGELOG.md already contains a '## [{version}]' section; "
            "refusing to write a duplicate"
        )

    # The next "## [" heading after Unreleased marks the end of its body.
    next_heading_idx = next(
        (
            i
            for i in range(unreleased_idx + 1, len(lines))
            if lines[i].startswith("## [")
        ),
        None,
    )
    if next_heading_idx is None:
        raise ValueError("could not find a heading after '## [Unreleased]'")

    # Body of the Unreleased section, including its own leading and trailing
    # blank line (e.g. ["", "### Changed", "", "- ...", ""]).
    old_body = lines[unreleased_idx + 1 : next_heading_idx]

    # "## [Unreleased]" stays in place (now with an empty body), followed by a
    # blank separator line, the new dated heading, and then the old body
    # verbatim -- its own leading blank line becomes the separator between the
    # new heading and its first "### " subsection, and its trailing blank
    # line still separates it from the next heading in the file.
    rebuilt = (
        lines[: unreleased_idx + 1]
        + [""]
        + [f"## [{version}] - {date}"]
        + old_body
        + lines[next_heading_idx:]
    )

    # Update the link-reference definitions at the bottom of the file.
    unreleased_link_re = re.compile(r"^\[Unreleased\]:\s")
    updated = []
    inserted_new_version_link = False
    for line in rebuilt:
        if unreleased_link_re.match(line):
            updated.append(f"[Unreleased]: {repo}/compare/v{version}...HEAD")
            updated.append(f"[{version}]: {repo}/compare/v{previous}...v{version}")
            inserted_new_version_link = True
            continue
        updated.append(line)

    if not inserted_new_version_link:
        raise ValueError("could not find a '[Unreleased]: ' link reference to update")

    return "\n".join(updated)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--changelog", required=True, help="Path to CHANGELOG.md")
    parser.add_argument("--version", required=True, help="New version, without leading v")
    parser.add_argument("--previous", required=True, help="Previous latest version, without leading v")
    parser.add_argument("--date", required=True, help="Release date, YYYY-MM-DD")
    parser.add_argument("--repo", required=True, help="Repository URL, no trailing slash")
    args = parser.parse_args()

    with open(args.changelog, "r", encoding="utf-8") as f:
        original = f.read()

    # A malformed changelog is an expected failure, not a crash: report it as a
    # one-line error the workflow log can show, rather than a traceback.
    try:
        rewritten = rewrite(original, args.version, args.previous, args.date, args.repo)
    except ValueError as err:
        print(f"error: {err}", file=sys.stderr)
        return 1

    with open(args.changelog, "w", encoding="utf-8") as f:
        f.write(rewritten)

    return 0


if __name__ == "__main__":
    sys.exit(main())
