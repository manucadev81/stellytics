import io
import json
import tarfile
import tempfile
import unittest
from pathlib import Path

from compare_layers import layers


def image(path, content, mtime, mode=0o755):
    layer_data = io.BytesIO()
    with tarfile.open(fileobj=layer_data, mode="w") as layer:
        item = tarfile.TarInfo("binary")
        item.size, item.mtime, item.mode = len(content), mtime, mode
        layer.addfile(item, io.BytesIO(content))
    files = {"manifest.json": json.dumps([{"Layers": ["layer.tar"]}]).encode(),
             "layer.tar": layer_data.getvalue()}
    with tarfile.open(path, "w") as out:
        for name, data in files.items():
            item = tarfile.TarInfo(name)
            item.size = len(data)
            out.addfile(item, io.BytesIO(data))


class CompareLayersTest(unittest.TestCase):
    def test_ignores_timestamps_but_compares_contents_and_permissions(self):
        with tempfile.TemporaryDirectory() as directory:
            paths = [Path(directory) / f"{i}.tar" for i in range(4)]
            image(paths[0], b"binary", 1)
            image(paths[1], b"binary", 2)
            image(paths[2], b"changed", 1)
            image(paths[3], b"binary", 1, 0o644)
            self.assertEqual(layers(paths[0]), layers(paths[1]))
            self.assertNotEqual(layers(paths[0]), layers(paths[2]))
            self.assertNotEqual(layers(paths[0]), layers(paths[3]))


if __name__ == "__main__":
    unittest.main()
