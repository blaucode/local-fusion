#!/usr/bin/env python3
"""Record a v1 review+judge round for the v2 replay parity harness (ADR-010).

Runs HOST-SIDE against the v1 checkout (v1's documented operating mode) using
v1's own Python environment. This is the record half; the replay half is
`make replay` in this repo.

Usage:
    python3 scripts/record-v1.py <v1-checkout> <fixture-dir>

<fixture-dir> must contain brief.md, changed_files.txt, test_report.json.
Produces there: recording.jsonl (one line per model call: request sans key +
response content) and artifacts/ (the v1-written slug tree).

Requires the LF_RECORD hook in v1 fusion/common.py::call_model (owner-approved
amendment, 2026-07-12). Live provider calls — needs v1 config/providers.env.
"""
import json
import os
import shutil
import sys
import tempfile
from pathlib import Path


def main():
    if len(sys.argv) != 3:
        print(__doc__, file=sys.stderr)
        return 2
    v1 = Path(sys.argv[1]).resolve()
    fixtures = Path(sys.argv[2]).resolve()
    sys.path.insert(0, str(v1 / "orchestrator"))

    from fusion.common import load_config, load_env
    from fusion.artifacts import init_slug, write_manifest, write_task_artifacts
    from fusion import review as fusion_review
    from fusion import judge as fusion_judge

    brief = (fixtures / "brief.md").read_text()
    changed = (fixtures / "changed_files.txt").read_text()
    test_report = json.loads((fixtures / "test_report.json").read_text())

    recording = fixtures / "recording.jsonl"
    if recording.exists():
        recording.unlink()
    os.environ["LF_RECORD"] = str(recording)

    cfg = load_config(root=v1)
    env = load_env(root=v1)

    with tempfile.TemporaryDirectory() as tmp:
        proj = Path(tmp) / "project"
        proj.mkdir()
        manifest = init_slug(proj, "hexcolor", brief, "main", "feature/hexcolor")
        manifest["tasks"].append({
            "id": "01",
            "slug": "parse",
            "title": "parse",
            "deps": [],
            "status": "planned",
            "scores": None,
        })
        write_manifest(proj, "hexcolor", manifest)
        write_task_artifacts(proj, "hexcolor", "01", "parse",
                             adr="", plan=brief, acceptance="", context="")

        fusion_review.review_task(str(proj), "hexcolor", "01", "parse",
                                  changed, cfg, env)
        fusion_judge.judge_task(str(proj), "hexcolor", "01", "parse",
                                changed, cfg, env, test_report=test_report)

        artifacts = fixtures / "artifacts"
        if artifacts.exists():
            shutil.rmtree(artifacts)
        shutil.copytree(proj / "local-fusion" / "hexcolor", artifacts)

    n = sum(1 for _ in recording.open())
    print(f"recorded {n} model calls → {recording}")
    print(f"v1 artifacts → {fixtures / 'artifacts'}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
