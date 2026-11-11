#!/usr/bin/env python3
"""Build the six standalone archives and checksums from a clean checkout."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import zipfile

ROOT = Path(__file__).resolve().parents[1]


def run(args, **kwargs):
    return subprocess.run(args, cwd=ROOT, check=True, text=True, **kwargs)


def notices(go):
    raw = run([go, "list", "-m", "-json", "all"], capture_output=True).stdout
    decoder = json.JSONDecoder()
    sections = []
    while raw.strip():
        module, end = decoder.raw_decode(raw.lstrip())
        raw = raw.lstrip()[end:]
        if module.get("Main") or not module.get("Dir"):
            continue
        directory = Path(module["Dir"])
        license_files = sorted(path for path in directory.iterdir() if path.is_file() and path.name.upper().startswith(("LICENSE", "COPYING", "NOTICE")))
        if not license_files:
            raise RuntimeError(f"Review license packaging for {module['Path']}")
        for license_file in license_files:
            sections.append(f"{module['Path']} {module['Version']} — {license_file.name}\n{'=' * 72}\n{license_file.read_text()}\n")
    go_root = Path(run([go, "env", "GOROOT"], capture_output=True).stdout.strip())
    sections.append("Go standard library\n" + "=" * 72 + "\n" + (go_root / "LICENSE").read_text())
    return "\n".join(sections)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("version", help="Release version without the v prefix")
    parser.add_argument("--output", default="dist")
    parser.add_argument("--go", default=str(ROOT / ".tools/go/bin/go") if (ROOT / ".tools/go/bin/go").is_file() else "go")
    args = parser.parse_args()
    if not re.fullmatch(r"\d+\.\d+\.\d+(?:-[A-Za-z0-9.-]+)?", args.version):
        parser.error("version must be a semantic version such as 0.2.0")
    if run(["git", "status", "--porcelain"], capture_output=True).stdout.strip():
        parser.error("commit the release files before packaging")
    output = (ROOT / args.output).resolve()
    output.mkdir(parents=True, exist_ok=True)
    notice = notices(args.go)
    archives = []
    for system in ("darwin", "linux", "windows"):
        for arch in ("amd64", "arm64"):
            name = f"launchproof_{args.version}_{system}_{arch}"
            stage = output / "staging" / name
            if stage.exists():
                parser.error(f"staging directory already exists: {stage}")
            stage.mkdir(parents=True)
            binary = stage / ("launchproof.exe" if system == "windows" else "launchproof")
            env = dict(os.environ, GOOS=system, GOARCH=arch, CGO_ENABLED="0")
            run([args.go, "build", "-trimpath", "-ldflags", f"-s -w -X main.version={args.version}", "-o", str(binary), "./cmd/launchproof"], env=env)
            for file in ("README.md", "LICENSE", "CHANGELOG.md", "CONTRIBUTING.md"):
                shutil.copy2(ROOT / file, stage / file)
            shutil.copytree(ROOT / "assets", stage / "assets")
            (stage / "THIRD_PARTY_NOTICES.txt").write_text(notice)
            files = sorted(path for path in stage.rglob("*") if path.is_file())
            archive = output / (name + (".zip" if system == "windows" else ".tar.gz"))
            if system == "windows":
                with zipfile.ZipFile(archive, "w", zipfile.ZIP_DEFLATED) as bundle:
                    for path in files:
                        bundle.write(path, path.relative_to(stage))
            else:
                with tarfile.open(archive, "w:gz") as bundle:
                    for path in files:
                        bundle.add(path, arcname=str(path.relative_to(stage)))
            archives.append(archive)
            print(f"Packaged {archive.name}", flush=True)
    (output / "checksums.txt").write_text("".join(f"{hashlib.sha256(path.read_bytes()).hexdigest()}  {path.name}\n" for path in archives))


if __name__ == "__main__":
    main()
