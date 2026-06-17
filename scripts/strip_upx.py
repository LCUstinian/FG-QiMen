#!/usr/bin/env python3
"""
strip_upx.py — In-place UPX signature removal for PE and ELF binaries.

Replaces UPX section names (UPX0/UPX1/UPX2) and version stamps (UPX!)
with harmless underscore padding. Does NOT change file size, does NOT
inject bloat data. The UPX decompression loader still works because it
locates sections by RVA, not by name.

Usage:
    python3 strip_upx.py <binary> [--dry-run]

Supports:
    - PE (Windows): strips UPX0/UPX1/UPX2 section names + UPX! stamp
    - ELF (Linux):  strips UPX section names + UPX version in .comment

Zero external dependencies (Python 3 stdlib only).
"""

import argparse
import struct
import sys
from pathlib import Path

# ── Constants ──────────────────────────────────────────────────────────

# Replacement strings (must be same length as originals for in-place swap)
REPL_4B = b"____"   # replaces "UPX0", "UPX1", "UPX2" (4 bytes)
REPL_4B_STAMP = b"\x00\x00\x00\x00"  # replaces "UPX!" stamp (4 bytes, null-padded safer)
REPL_3B = b"___"    # replaces "UPX" in ELF .comment (3 bytes)

# PE magic bytes
PE_MAGIC = b"MZ"
PE_NT_SIGNATURE = b"PE\x00\x00"

# ELF magic bytes
ELF_MAGIC = b"\x7fELF"

# UPX patterns to search for
UPX_SECTION_NAMES = [b"UPX0", b"UPX1", b"UPX2"]
UPX_STAMP = b"UPX!"
UPX_COMMENT_PREFIX = b"UPX "


# ── PE Handler ─────────────────────────────────────────────────────────

def strip_pe(data: bytearray, dry_run: bool = False) -> int:
    """
    Strip UPX signatures from a PE binary.

    Returns the number of replacements made.
    """
    replacements = 0

    # Validate DOS header
    if data[:2] != PE_MAGIC:
        return 0

    # Read e_lfanew (offset to NT headers) at DOS header offset 0x3C
    e_lfanew = struct.unpack_from("<I", data, 0x3C)[0]
    if e_lfanew + 4 > len(data):
        return 0

    # Validate NT signature
    if data[e_lfanew:e_lfanew + 4] != PE_NT_SIGNATURE:
        return 0

    # ── Strip section names ────────────────────────────────────────────
    # COFF File Header starts right after NT signature (4 bytes)
    # COFF File Header is 20 bytes:
    #   [0:2]  Machine
    #   [2:4]  NumberOfSections
    #   [4:8]  TimeDateStamp
    #   [8:12] PointerToSymbolTable
    #   [12:16] NumberOfSymbols
    #   [16:18] SizeOfOptionalHeader
    #   [18:20] Characteristics
    coff_offset = e_lfanew + 4
    if coff_offset + 20 > len(data):
        return 0

    num_sections = struct.unpack_from("<H", data, coff_offset + 2)[0]
    size_of_optional_header = struct.unpack_from("<H", data, coff_offset + 16)[0]

    # Section headers start after COFF header + optional header
    section_headers_offset = coff_offset + 20 + size_of_optional_header

    # Each section header is 40 bytes:
    #   [0:8]  Name (8 bytes, null-padded)
    #   [8:12] VirtualSize
    #   [12:16] VirtualAddress
    #   [16:20] SizeOfRawData
    #   [20:24] PointerToRawData
    #   ...
    for i in range(num_sections):
        sec_offset = section_headers_offset + i * 40
        if sec_offset + 8 > len(data):
            break

        sec_name = data[sec_offset:sec_offset + 8]
        # Strip trailing nulls for comparison
        sec_name_stripped = sec_name.rstrip(b"\x00")

        if sec_name_stripped in UPX_SECTION_NAMES:
            if not dry_run:
                data[sec_offset:sec_offset + 4] = REPL_4B
            replacements += 1
            print(f"  [PE] Section {i}: '{sec_name_stripped.decode('ascii', errors='replace')}' -> '____'")

    # ── Strip UPX! stamp ───────────────────────────────────────────────
    # The UPX! stamp is a 4-byte marker placed by UPX in the binary.
    # It's typically located near the end of the headers or in the
    # overlay. We do a simple byte-scan for it.
    #
    # Strategy: scan the first 64KB of the file (headers + early sections)
    # for the "UPX!" pattern. This covers the common locations without
    # scanning the entire file.
    scan_limit = min(len(data), 65536)
    offset = 0
    while True:
        idx = data.find(UPX_STAMP, offset, scan_limit)
        if idx == -1:
            break
        if not dry_run:
            data[idx:idx + 4] = REPL_4B_STAMP
        replacements += 1
        print(f"  [PE] UPX! stamp at offset 0x{idx:08X} -> null")
        offset = idx + 4

    return replacements


# ── ELF Handler ────────────────────────────────────────────────────────

def strip_elf(data: bytearray, dry_run: bool = False) -> int:
    """
    Strip UPX signatures from an ELF binary.

    Returns the number of replacements made.
    """
    replacements = 0

    # Validate ELF magic
    if data[:4] != ELF_MAGIC:
        return 0

    # ELF class: 1 = 32-bit, 2 = 64-bit
    ei_class = data[4]
    if ei_class not in (1, 2):
        return 0

    is_64 = ei_class == 2

    # ELF header field sizes depend on class
    if is_64:
        # 64-bit ELF
        # e_shoff at offset 0x28 (8 bytes)
        # e_shentsize at offset 0x3A (2 bytes)
        # e_shnum at offset 0x3C (2 bytes)
        # e_shstrndx at offset 0x3E (2 bytes)
        e_shoff = struct.unpack_from("<Q", data, 0x28)[0]
        e_shentsize = struct.unpack_from("<H", data, 0x3A)[0]
        e_shnum = struct.unpack_from("<H", data, 0x3C)[0]
        e_shstrndx = struct.unpack_from("<H", data, 0x3E)[0]
    else:
        # 32-bit ELF
        # e_shoff at offset 0x20 (4 bytes)
        # e_shentsize at offset 0x2E (2 bytes)
        # e_shnum at offset 0x30 (2 bytes)
        # e_shstrndx at offset 0x32 (2 bytes)
        e_shoff = struct.unpack_from("<I", data, 0x20)[0]
        e_shentsize = struct.unpack_from("<H", data, 0x2E)[0]
        e_shnum = struct.unpack_from("<H", data, 0x30)[0]
        e_shstrndx = struct.unpack_from("<H", data, 0x32)[0]

    if e_shoff == 0 or e_shnum == 0:
        return 0

    # Read the section header string table (.shstrtab) to find section names
    if is_64:
        shstrtab_offset = struct.unpack_from("<Q", data, e_shoff + e_shstrndx * e_shentsize + 0x18)[0]
        shstrtab_size = struct.unpack_from("<Q", data, e_shoff + e_shstrndx * e_shentsize + 0x20)[0]
    else:
        shstrtab_offset = struct.unpack_from("<I", data, e_shoff + e_shstrndx * e_shentsize + 0x10)[0]
        shstrtab_size = struct.unpack_from("<I", data, e_shoff + e_shstrndx * e_shentsize + 0x14)[0]

    shstrtab = data[shstrtab_offset:shstrtab_offset + shstrtab_size]

    def get_section_name(name_offset: int) -> bytes:
        end = shstrtab.find(b"\x00", name_offset)
        if end == -1:
            return shstrtab[name_offset:]
        return shstrtab[name_offset:end]

    # Iterate section headers
    for i in range(e_shnum):
        sh_offset = e_shoff + i * e_shentsize
        if sh_offset + e_shentsize > len(data):
            break

        if is_64:
            sh_name_idx = struct.unpack_from("<I", data, sh_offset)[0]
            sh_type = struct.unpack_from("<I", data, sh_offset + 4)[0]
            sh_data_offset = struct.unpack_from("<Q", data, sh_offset + 0x18)[0]
            sh_size = struct.unpack_from("<Q", data, sh_offset + 0x20)[0]
        else:
            sh_name_idx = struct.unpack_from("<I", data, sh_offset)[0]
            sh_type = struct.unpack_from("<I", data, sh_offset + 4)[0]
            sh_data_offset = struct.unpack_from("<I", data, sh_offset + 0x10)[0]
            sh_size = struct.unpack_from("<I", data, sh_offset + 0x14)[0]

        sec_name = get_section_name(sh_name_idx)

        # Strip UPX section names (UPX0, UPX1, UPX2)
        if sec_name in UPX_SECTION_NAMES:
            # Section name is in the string table, not in the section header itself
            # We need to replace it in the shstrtab area
            name_in_shstrtab = shstrtab_offset + sh_name_idx
            if not dry_run:
                data[name_in_shstrtab:name_in_shstrtab + 4] = REPL_4B
            replacements += 1
            print(f"  [ELF] Section '{sec_name.decode('ascii', errors='replace')}' -> '____'")

        # Strip UPX version string in .comment section (SHT_PROGBITS = 1)
        if sec_name == b".comment" and sh_type == 1:
            comment_data = data[sh_data_offset:sh_data_offset + sh_size]
            # Find "UPX " prefix in the comment data
            upx_idx = comment_data.find(UPX_COMMENT_PREFIX)
            if upx_idx != -1:
                # Replace "UPX " with "____" (4 bytes -> 4 bytes)
                abs_offset = sh_data_offset + upx_idx
                if not dry_run:
                    data[abs_offset:abs_offset + 4] = REPL_4B
                replacements += 1
                version_str = comment_data[upx_idx:upx_idx + 20].rstrip(b"\x00").decode("ascii", errors="replace")
                print(f"  [ELF] .comment UPX version '{version_str}' -> '____'")

    return replacements


# ── Main ───────────────────────────────────────────────────────────────

def detect_format(data: bytes) -> str:
    """Detect binary format from magic bytes."""
    if data[:2] == PE_MAGIC:
        return "PE"
    if data[:4] == ELF_MAGIC:
        return "ELF"
    return "unknown"


def main():
    parser = argparse.ArgumentParser(
        description="Strip UPX signatures from PE/ELF binaries (in-place, zero dependencies)."
    )
    parser.add_argument("binary", help="Path to the UPX-compressed binary")
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Show what would be replaced without modifying the file",
    )
    args = parser.parse_args()

    path = Path(args.binary)
    if not path.exists():
        print(f"Error: file not found: {path}", file=sys.stderr)
        sys.exit(1)

    data = bytearray(path.read_bytes())
    fmt = detect_format(data)

    print(f"[*] {path} — format: {fmt}, size: {len(data):,} bytes")

    if fmt == "PE":
        replacements = strip_pe(data, args.dry_run)
    elif fmt == "ELF":
        replacements = strip_elf(data, args.dry_run)
    else:
        print("[*] Unsupported format (not PE or ELF) — skipping")
        sys.exit(0)

    if replacements == 0:
        print("[*] No UPX signatures found — file may not be UPX-compressed")
        sys.exit(0)

    print(f"[*] {replacements} replacement(s) made")

    if not args.dry_run:
        path.write_bytes(data)
        print(f"[*] File written: {path}")
    else:
        print("[*] Dry run — no changes written")


if __name__ == "__main__":
    main()
