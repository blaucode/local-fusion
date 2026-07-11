#!/usr/bin/env python3
"""Verify every relative markdown link in the repo resolves. Used by `make docs-check` and CI."""
import os
import re
import sys

bad = []
for root, dirs, files in os.walk("."):
    dirs[:] = [d for d in dirs if d not in (".git", "bin", "node_modules")]
    for f in files:
        if not f.endswith(".md"):
            continue
        p = os.path.join(root, f)
        with open(p, encoding="utf-8") as fh:
            content = fh.read()
        for m in re.finditer(r"\]\((\.\.?/[^)#]+)", content):
            target = os.path.normpath(os.path.join(root, m.group(1)))
            if not os.path.exists(target):
                bad.append(f"{p} -> {m.group(1)}")

if bad:
    print("broken links:")
    for b in bad:
        print(f"  {b}")
    sys.exit(1)
print(f"all relative links resolve")
