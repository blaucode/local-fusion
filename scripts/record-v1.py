#!/usr/bin/env python3
"""Record a v1 review+judge round for the v2 replay parity harness (ADR-010).

Runs HOST-SIDE against the v1 checkout (v1's documented operating mode) using
v1's own Python environment. This is the record half; the replay half is
`make replay` in this repo.

Usage:
    python3 scripts/record-v1.py <v1-checkout> <fixture-dir> [mode]

Modes:
    review-judge (default) — fixture-dir must contain brief.md,
        changed_files.txt, test_report.json.
    plan-solo — fixture-dir must contain request.txt, context.txt, slug.txt;
        runs v1 plan_feature(no_fusion=True) against a scratch git repo (v1's
        gitops is real, so the recorder fakes the repo, not the engine).
    plan-full — same inputs; runs plan_feature(no_fusion=False): decompose +
        haft + TL panel + synthesizer.
    plan-full-degraded — plan-full with the synthesizer call failed by the
        recorder harness (v1 source untouched): records the injected-failure
        degradation path (ADR-010) with a {"failed": true} sentinel line.
    coder-solo — fixture-dir must contain plan.md, acceptance.md, context.txt,
        slug.txt; seeds a planned task and runs coder_fusion_task(solo=True).
    coder-fusion — same inputs; the full two-coder + evaluator + lead path.
        The two coder calls run in parallel threads, so they land in the
        recording in completion order; the v2 replay matches them
        order-tolerantly.
    coder-fusion-degraded — coder-fusion with coder-b failed by the recorder
        harness (both attempts recorded as {"failed": true} sentinels):
        exercises the survivor degradation path deterministically.

The recorder wraps call_model so recording.jsonl is a COMPLETE transcript:
v1's internal LF_RECORD hook writes successes; the wrapper writes a
{"failed": true} sentinel for any call that returns None (a natural timeout,
or an injected failure). Without this a naturally-failed call leaves no trace
and the replay cannot reproduce v1's degradation. v1 source is untouched.

Produces in fixture-dir: recording.jsonl (one line per model call: request
sans key + response content, or a failed sentinel) and artifacts/.

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
    if len(sys.argv) not in (3, 4):
        print(__doc__, file=sys.stderr)
        return 2
    v1 = Path(sys.argv[1]).resolve()
    fixtures = Path(sys.argv[2]).resolve()
    mode = sys.argv[3] if len(sys.argv) == 4 else "review-judge"
    sys.path.insert(0, str(v1 / "orchestrator"))

    if mode in ("plan-solo", "plan-full", "plan-full-degraded"):
        return record_plan(v1, fixtures, mode)
    if mode in ("coder-solo", "coder-fusion", "coder-fusion-degraded"):
        return record_coder(v1, fixtures, mode)
    return record_review_judge(v1, fixtures)


def record_coder(v1, fixtures, mode):
    import threading

    from fusion import common as fusion_common
    from fusion.common import load_config, load_env
    from fusion.artifacts import init_slug, read_manifest, write_manifest, write_task_artifacts
    from fusion import coder_fusion as fusion_coder

    plan_md = (fixtures / "plan.md").read_text()
    acceptance = (fixtures / "acceptance.md").read_text()
    context = (fixtures / "context.txt").read_text()
    slug = (fixtures / "slug.txt").read_text().strip()

    recording = fixtures / "recording.jsonl"
    if recording.exists():
        recording.unlink()
    os.environ["LF_RECORD"] = str(recording)

    cfg = load_config(root=v1)
    env = load_env(root=v1)

    # Complete-transcript wrapper: v1's LF_RECORD hook writes successes; this
    # writes a failed sentinel for any None return (natural or injected). The
    # coders run in parallel threads, so appends are locked. v1 is untouched.
    real_call_model = fusion_common.call_model
    lock = threading.Lock()

    def record_failure(model_cfg, provider_cfg, messages, max_tokens, timeout):
        with lock, open(recording, "a") as f:
            f.write(json.dumps({
                "model_id": model_cfg["id"],
                "base_url": provider_cfg["base_url"].rstrip("/"),
                "messages": messages,
                "max_tokens": max_tokens,
                "timeout": timeout,
                "failed": True,
            }) + "\n")

    def wrapped_call_model(model_cfg, provider_cfg, env_, messages,
                           max_tokens=8192, label=None, timeout=190):
        if mode == "coder-fusion-degraded" and label and label.startswith("coder-b"):
            record_failure(model_cfg, provider_cfg, messages, max_tokens, timeout)
            return None
        out = real_call_model(model_cfg, provider_cfg, env_, messages,
                              max_tokens=max_tokens, label=label, timeout=timeout)
        if out is None:
            record_failure(model_cfg, provider_cfg, messages, max_tokens, timeout)
        return out

    fusion_coder.call_model = wrapped_call_model

    with tempfile.TemporaryDirectory() as tmp:
        proj = Path(tmp) / "project"
        proj.mkdir()
        init_slug(str(proj), slug, "recorded coder parity case", "main", f"feature/{slug}")
        manifest = read_manifest(str(proj), slug)
        manifest["tasks"] = [{"id": "01", "slug": "impl", "title": "impl",
                              "deps": [], "status": "planned", "scores": None}]
        write_manifest(str(proj), slug, manifest)
        write_task_artifacts(str(proj), slug, "01", "impl", "", plan_md, acceptance, context)

        fusion_coder.coder_fusion_task(str(proj), slug, "01", "impl", context, cfg, env,
                                       solo=(mode == "coder-solo"))

        artifacts = fixtures / "artifacts"
        if artifacts.exists():
            shutil.rmtree(artifacts)
        shutil.copytree(proj / "local-fusion" / slug, artifacts)

    n = sum(1 for _ in recording.open())
    print(f"recorded {n} model calls → {recording}")
    print(f"v1 artifacts → {fixtures / 'artifacts'}")
    return 0


def record_plan(v1, fixtures, mode):
    import subprocess

    from fusion import common as fusion_common
    from fusion.common import load_config, load_env
    from fusion import plan as fusion_plan

    request = (fixtures / "request.txt").read_text()
    context = (fixtures / "context.txt").read_text()
    slug = (fixtures / "slug.txt").read_text().strip()

    recording = fixtures / "recording.jsonl"
    if recording.exists():
        recording.unlink()
    os.environ["LF_RECORD"] = str(recording)

    cfg = load_config(root=v1)
    env = load_env(root=v1)

    if mode == "plan-full-degraded":
        # Recorder-harness injection (v1 source untouched): fail exactly the
        # synthesizer call, record it as a failed sentinel, let everything
        # else hit the real providers and record normally.
        real_call_model = fusion_common.call_model

        def failing_call_model(model_cfg, provider_cfg, env_, messages,
                               max_tokens=8192, label=None, timeout=190):
            if label == "synthesize-plan":
                with open(recording, "a") as f:
                    f.write(json.dumps({
                        "model_id": model_cfg["id"],
                        "base_url": provider_cfg["base_url"].rstrip("/"),
                        "messages": messages,
                        "max_tokens": max_tokens,
                        "timeout": timeout,
                        "failed": True,
                    }) + "\n")
                return None
            return real_call_model(model_cfg, provider_cfg, env_, messages,
                                   max_tokens=max_tokens, label=label, timeout=timeout)

        fusion_plan.call_model = failing_call_model

    with tempfile.TemporaryDirectory() as tmp:
        proj = Path(tmp) / "project"
        proj.mkdir()
        # v1's gitops is real: give it a real (scratch) repo on branch main.
        for args in (
            ["init", "-b", "main"],
            ["config", "user.email", "parity@local"],
            ["config", "user.name", "parity"],
        ):
            subprocess.run(["git", "-C", str(proj), *args], check=True, capture_output=True)
        (proj / "README.md").write_text("scratch repo for v1 plan recording\n")
        subprocess.run(["git", "-C", str(proj), "add", "-A"], check=True, capture_output=True)
        subprocess.run(["git", "-C", str(proj), "commit", "-m", "init"], check=True, capture_output=True)

        fusion_plan.plan_feature(str(proj), slug, request, context, cfg, env,
                                 no_fusion=(mode == "plan-solo"))

        artifacts = fixtures / "artifacts"
        if artifacts.exists():
            shutil.rmtree(artifacts)
        shutil.copytree(proj / "local-fusion" / slug, artifacts)

    n = sum(1 for _ in recording.open())
    print(f"recorded {n} model calls → {recording}")
    print(f"v1 artifacts → {fixtures / 'artifacts'}")
    return 0


def record_review_judge(v1, fixtures):
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
