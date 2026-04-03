#!/usr/bin/env python3
"""Update SHA256 checksums in the Krew manifest after GoReleaser builds."""

import re
import sys
from pathlib import Path

CHECKSUMS_FILE = Path("dist/checksums.txt")
MANIFEST_FILE = Path("plugins/rustnet.yaml")

ARCHIVE_MAP = {
    "kubectl-rustnet_linux_amd64.tar.gz": "linux.*amd64",
    "kubectl-rustnet_linux_arm64.tar.gz": "linux.*arm64",
    "kubectl-rustnet_darwin_amd64.tar.gz": "darwin.*amd64",
    "kubectl-rustnet_darwin_arm64.tar.gz": "darwin.*arm64",
    "kubectl-rustnet_windows_amd64.zip": "windows.*amd64",
}


def main():
    if not CHECKSUMS_FILE.exists():
        print(f"Error: {CHECKSUMS_FILE} not found. Run GoReleaser first.", file=sys.stderr)
        sys.exit(1)

    # Parse checksums
    checksums = {}
    for line in CHECKSUMS_FILE.read_text().strip().split("\n"):
        parts = line.split()
        if len(parts) == 2:
            checksums[parts[1]] = parts[0]

    # Read manifest
    manifest = MANIFEST_FILE.read_text()

    # Replace PLACEHOLDER checksums
    for archive, _ in ARCHIVE_MAP.items():
        if archive in checksums:
            # Find the URI line containing this archive and replace the next sha256 line
            lines = manifest.split("\n")
            for i, line in enumerate(lines):
                if archive in line:
                    # Next line with sha256
                    for j in range(i + 1, min(i + 3, len(lines))):
                        if "sha256:" in lines[j]:
                            lines[j] = f"      sha256: {checksums[archive]}"
                            break
            manifest = "\n".join(lines)

    MANIFEST_FILE.write_text(manifest)
    print("Krew manifest updated with checksums.")


if __name__ == "__main__":
    main()
