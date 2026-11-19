"""Apply Docker daemon defaults for the VPS installation."""

import json
import os
import tempfile

path = "/etc/docker/daemon.json"
os.makedirs(os.path.dirname(path), mode=0o755, exist_ok=True)
config = {}
if os.path.exists(path):
    with open(path, encoding="utf-8") as stream:
        config = json.load(stream)

desired = {
    "userland-proxy": False,
    "ipv6": True,
    "ip6tables": True,
    "live-restore": True,
    "log-driver": "json-file",
    "log-opts": {"max-size": "20m", "max-file": "5"},
}
changed = any(config.get(key) != value for key, value in desired.items())
config.update(desired)
if changed:
    fd, temporary = tempfile.mkstemp(prefix="daemon.json.", dir=os.path.dirname(path))
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as stream:
            json.dump(config, stream, indent=2)
            stream.write("\n")
        os.chmod(temporary, 0o644)
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)
print("changed" if changed else "unchanged")
