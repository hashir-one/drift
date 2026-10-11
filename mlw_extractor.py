"""Authenticated MLW conversion with non-destructive output publication."""
import os
from pathlib import Path
import sys
import tempfile

from cryptography.hazmat.primitives.ciphers.aead import AESGCM

MAX_SIZE = 512 * 1024 * 1024
KEY = bytes.fromhex("d27e154628ae2ba6ab4b9775165ff737")


def extract_mlw(input_path):
    source = Path(input_path)
    if source.suffix.lower() != ".mlw":
        raise ValueError("select an .mlw file")
    if not source.is_file() or source.stat().st_size > MAX_SIZE:
        raise ValueError("select a regular MLW file smaller than 512 MiB")
    with source.open("rb") as stream:
        data = stream.read(MAX_SIZE + 1)
    if len(data) > MAX_SIZE:
        raise ValueError("file exceeds the 512 MiB limit")
    root = data.find(b"Root\x00")
    if root < 0:
        raise ValueError("root marker not found")
    end = data.find(b"\x00", root + 5)
    if end < 0 or end + 13 > len(data) - 33:
        raise ValueError("incomplete MLW header or payload")
    offset = end + 13
    # AESGCM.decrypt checks the trailing authentication tag before returning data.
    plaintext = AESGCM(KEY).decrypt(data[offset:offset + 12], data[offset + 16:], None)
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(dir=source.parent, prefix=".mlw-output-", delete=False) as out:
            temporary = Path(out.name)
            out.write(plaintext)
            out.flush()
            os.fsync(out.fileno())
        for number in range(10000):
            suffix = f" ({number})" if number else ""
            destination = source.with_name(source.stem + suffix + ".mp4")
            try:
                # Hard-link publication is atomic and fails if the name exists.
                os.link(temporary, destination)
                return destination
            except FileExistsError:
                continue
        raise FileExistsError("too many outputs with the same name")
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)


if __name__ == "__main__":
    if len(sys.argv) != 2:
        print("usage: python mlw_extractor.py <file.mlw>", file=sys.stderr)
        sys.exit(2)
    try:
        print(f"Video saved to: {extract_mlw(sys.argv[1])}")
    except Exception as error:
        print(f"Conversion failed: {error or type(error).__name__}", file=sys.stderr)
        sys.exit(1)
