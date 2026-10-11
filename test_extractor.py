import concurrent.futures
from pathlib import Path
import tempfile
import unittest

from cryptography.exceptions import InvalidTag
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
from mlw_extractor import KEY, extract_mlw


class ExtractorTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.path = Path(self.tmp.name) / "sample.MLW"
        nonce = bytes(12)
        self.data = (b"Root\0sample.mp4\0" + bytes(12) + nonce + bytes(4)
                     + AESGCM(KEY).encrypt(nonce, b"authenticated video", None))
        self.path.write_bytes(self.data)

    def test_preserves_existing(self):
        old = self.path.with_suffix(".mp4")
        old.write_bytes(b"original")
        out = extract_mlw(self.path)
        self.assertNotEqual(out, old)
        self.assertEqual(old.read_bytes(), b"original")
        self.assertEqual(out.read_bytes(), b"authenticated video")

    def test_rejects_tampering(self):
        bad = bytearray(self.data)
        bad[-1] ^= 1
        self.path.write_bytes(bad)
        with self.assertRaises(InvalidTag):
            extract_mlw(self.path)
        self.assertEqual(list(self.path.parent.glob("*.mp4")), [])

    def test_rejects_truncated_header(self):
        self.path.write_bytes(b"Root\0x\0")
        with self.assertRaises(ValueError):
            extract_mlw(self.path)

    def test_concurrent_outputs_are_unique(self):
        with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
            outputs = list(pool.map(extract_mlw, [self.path] * 8))
        self.assertEqual(len(set(outputs)), 8)
        self.assertTrue(all(p.read_bytes() == b"authenticated video" for p in outputs))


if __name__ == "__main__":
    unittest.main()
