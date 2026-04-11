#!/usr/bin/env python3
"""Create bakefile from 'docker compose config --format json' output"""
from typing import Dict, Any, Sequence, Tuple
import asyncio
import datetime
import os

PLATFORMS = ("linux/amd64", "linux/arm64")
ISODATE = datetime.datetime.now(datetime.UTC).date().isoformat()
ORIG_REPO = "ghcr.io"
ALT_REPOS = ("docker.io", os.environ.get("ACR_REPO", None))
DOCKER_TAG_EXTRA = os.environ.get("DOCKER_TAG_EXTRA", "")


def service_hcl(
    servicename: str, servicedef: Dict[str, Any]
) -> Tuple[Sequence[str], str]:
    """Make the HCL"""
    hcl_targets = ""
    tgtname = servicename
    imgtags_orig = [f"{servicedef['image']}", f"{servicedef['image']}-{ISODATE}"]
    if not DOCKER_TAG_EXTRA:
        imgtags_orig.append("ghcr.io/pvarki/kc_client_init:latest")
    imgtags_more = []
    for alt_repo in ALT_REPOS:
        if not alt_repo:
            continue
        imgtags_more += [tag.replace(ORIG_REPO, alt_repo) for tag in imgtags_orig]
    imgtags = imgtags_orig + imgtags_more
    hcl_targets += f"""
target "{tgtname}" {{
    tags = [{", ".join(f'"{imgtag}"' for imgtag in imgtags)}]
    dockerfile = "{servicedef['build']['dockerfile']}"
    context = "{servicedef['build']['context']}"
    platforms = [{", ".join(f'"{platform}"' for platform in PLATFORMS)}]
"""
    if "target" in servicedef["build"]:
        hcl_targets += f"""    target = "{servicedef['build']['target']}"\n"""

    if "args" in servicedef["build"]:
        hcl_targets += "    args = {\n"
        for argname, argval in servicedef["build"]["args"].items():
            hcl_targets += f"""        {argname}: "{argval}"\n"""
        hcl_targets += "    }\n"

    hcl_targets += "}"
    return [tgtname], hcl_targets


async def main() -> None:
    """Main entry point."""
    ret_tgts, ret_hcl = service_hcl(
        "kc_client_init",
        {
            "image": f"ghcr.io/pvarki/kc_client_init:1.0.0{DOCKER_TAG_EXTRA}",
            "build": {
                "context": "./",
                "dockerfile": "Dockerfile",
                "target": "production",
            },
        },
    )
    print(
        f"""
group "default" {{
    targets = [{", ".join(f'"{tgt}"' for tgt in ret_tgts)}]
}}
"""
    )
    print(ret_hcl)


if __name__ == "__main__":
    asyncio.run(main())
