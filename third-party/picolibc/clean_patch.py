#!/usr/bin/env python3
"""
clean_patch.py — Remove unnecessary hunks from gaston-picolibc.patch.

Removes two categories of hunks:
1. Whitespace-only changes: where the only difference is removal of spaces
   before '(' (between identifier/') and '(') or after cast ')'.
2. FALLTHROUGH comment-to-statement conversions: where '-' lines have
   /* FALLTHRU */ or /* FALLTHROUGH */ comments and '+' lines add
   __fallthrough; or FALLTHROUGH; statements (and the minus-line content
   without the comment otherwise matches the plus-line content).

Hunks with ANY change that doesn't fit either pattern are left alone.
If a file section ends up with no hunks, the entire section is removed.
"""

import re
import shutil
import sys

PATCH_PATH = '/Users/iansmith/gaston/third-party/picolibc/gaston-picolibc.patch'
BACKUP_PATH = PATCH_PATH + '.bak'


# ---------------------------------------------------------------------------
# Whitespace normalization
# ---------------------------------------------------------------------------

def normalize_ws(line: str) -> str:
    """Apply the two whitespace normalization rules to a line."""
    # Remove space before '('  — identifier/digit/closing-paren followed by spaces then '('
    line = re.sub(r'([a-zA-Z_0-9\)])\s+\(', r'\1(', line)
    # Remove space after cast ')' — ')' followed by spaces then identifier/open-paren/quote
    line = re.sub(r'\)\s+([a-zA-Z_0-9\("\'_])', r')\1', line)
    return line


def is_whitespace_only_hunk(minus_lines: list[str], plus_lines: list[str]) -> bool:
    """
    Return True iff the hunk is purely a whitespace normalization change.

    Conditions:
    - Equal number of minus and plus lines.
    - After applying normalize_ws(), each minus line equals the corresponding
      plus line (comparison is line-for-line, preserving trailing newlines).
    """
    if len(minus_lines) != len(plus_lines):
        return False
    if not minus_lines:
        return False
    return all(normalize_ws(m) == normalize_ws(p)
               for m, p in zip(minus_lines, plus_lines))


# ---------------------------------------------------------------------------
# FALLTHROUGH detection
# ---------------------------------------------------------------------------

# Patterns for lines that carry a FALLTHRU/FALLTHROUGH comment
_FALLTHRU_COMMENT_RE = re.compile(
    r'/\*\s*FALLTH(?:RU|ROUGH)\s*\*/'
)

# Acceptable '+' replacements
_FALLTHROUGH_STMT_RE = re.compile(
    r'^\s*(?:__fallthrough|FALLTHROUGH)\s*;'
)


def _strip_fallthru_comment(content: str) -> str:
    """Remove a trailing FALLTHRU/FALLTHROUGH comment and trailing whitespace."""
    return _FALLTHRU_COMMENT_RE.sub('', content).rstrip()


def is_fallthrough_only_hunk(minus_lines: list[str], plus_lines: list[str]) -> bool:
    """
    Return True iff every change in the hunk is a FALLTHROUGH comment →
    __fallthrough; statement conversion (and nothing else changed).

    The pattern is:
      -    some_expr; /* FALLTHRU */
      +    some_expr;
      +    __fallthrough;

    OR the minus line is a standalone comment line:
      -    /* FALLTHRU */
      +    __fallthrough;

    We pair up minus lines with their plus counterparts:
    - For each minus line that ends with a FALLTHRU comment:
        * If the line is ONLY the comment, the corresponding plus line must be
          exactly the __fallthrough; statement.
        * If the line has code + the comment, the corresponding plus lines must
          be: (a) the same code without the comment, and (b) the __fallthrough;
          statement.
    - Any minus line WITHOUT a FALLTHRU comment must have an identical plus line
      (i.e. it is a context change that got absorbed into this hunk, which we
      treat as "not purely FALLTHROUGH" — be conservative).
    """
    if not minus_lines and not plus_lines:
        return False

    # Walk through minus and plus lines together
    mi = 0  # index into minus_lines
    pi = 0  # index into plus_lines

    while mi < len(minus_lines):
        m = minus_lines[mi]
        m_content = m  # already stripped of leading '-', still has '\n'

        if _FALLTHRU_COMMENT_RE.search(m_content):
            stripped = _strip_fallthru_comment(m_content)  # code without comment

            if not stripped.strip():
                # The minus line is just the comment (possibly with indentation)
                # Expect exactly one plus line: the __fallthrough; statement
                if pi >= len(plus_lines):
                    return False
                p = plus_lines[pi]
                if not _FALLTHROUGH_STMT_RE.match(p):
                    return False
                pi += 1
            else:
                # The minus line has code + comment.
                # Expect two plus lines:
                #   1. same code without the comment  (stripped + '\n')
                #   2. __fallthrough; statement
                if pi + 1 >= len(plus_lines):
                    return False
                p1 = plus_lines[pi]
                p2 = plus_lines[pi + 1]
                # p1 should match stripped (with trailing newline)
                expected_p1 = stripped + '\n' if not stripped.endswith('\n') else stripped
                if p1.rstrip('\n') != stripped:
                    return False
                if not _FALLTHROUGH_STMT_RE.match(p2):
                    return False
                pi += 2
        else:
            # No FALLTHRU comment on this minus line — it must have an
            # identical plus line (be conservative: any other change disqualifies).
            if pi >= len(plus_lines):
                return False
            if minus_lines[mi] != plus_lines[pi]:
                return False
            pi += 1

        mi += 1

    # All plus lines must have been consumed
    return pi == len(plus_lines)


# ---------------------------------------------------------------------------
# Patch parser / writer
# ---------------------------------------------------------------------------

def parse_and_clean(lines: list[str]):
    """
    Parse the patch into file sections and hunks, remove unnecessary hunks,
    and return (cleaned_lines, stats_dict).
    """
    # Each file section: list of (header_lines, list_of_hunks)
    # header_lines: everything up to (but not including) the first @@ of that section
    # each hunk: (hunk_header_line, list_of_body_lines)

    sections = []   # list of {'header': [...], 'hunks': [...]}
    current_section = None
    current_hunk = None

    for line in lines:
        if line.startswith('diff '):
            # Start a new file section
            if current_hunk is not None:
                current_section['hunks'].append(current_hunk)
                current_hunk = None
            if current_section is not None:
                sections.append(current_section)
            current_section = {'header': [line], 'hunks': []}
        elif line.startswith('@@'):
            # Start a new hunk within the current section
            if current_section is None:
                # Patch starts with a hunk (no diff header) — shouldn't happen but be safe
                current_section = {'header': [], 'hunks': []}
                sections.append(current_section)
            if current_hunk is not None:
                current_section['hunks'].append(current_hunk)
            current_hunk = {'header': line, 'body': []}
        else:
            # Regular line — either file header or hunk body
            if current_hunk is not None:
                current_hunk['body'].append(line)
            elif current_section is not None:
                current_section['header'].append(line)
            # else: lines before any 'diff' — ignore (shouldn't happen)

    # Flush last hunk and section
    if current_hunk is not None and current_section is not None:
        current_section['hunks'].append(current_hunk)
    if current_section is not None:
        sections.append(current_section)

    # Now filter hunks
    total_hunks = 0
    removed_ws = 0
    removed_ft = 0
    kept_hunks = 0
    empty_files = 0

    output_lines = []

    for section in sections:
        kept = []
        for hunk in section['hunks']:
            total_hunks += 1
            body = hunk['body']
            minus_lines = [l[1:] for l in body if l.startswith('-')]
            plus_lines  = [l[1:] for l in body if l.startswith('+')]

            if is_whitespace_only_hunk(minus_lines, plus_lines):
                removed_ws += 1
            elif is_fallthrough_only_hunk(minus_lines, plus_lines):
                removed_ft += 1
            else:
                kept.append(hunk)
                kept_hunks += 1

        if not kept:
            # All hunks removed — skip this entire file section
            empty_files += 1
            continue

        # Write the section header
        output_lines.extend(section['header'])
        # Write kept hunks
        for hunk in kept:
            output_lines.append(hunk['header'])
            output_lines.extend(hunk['body'])

    stats = {
        'total_hunks': total_hunks,
        'removed_ws': removed_ws,
        'removed_ft': removed_ft,
        'kept_hunks': kept_hunks,
        'empty_files': empty_files,
        'total_files': len(sections),
    }
    return output_lines, stats


def main():
    print(f"Reading patch: {PATCH_PATH}")

    with open(PATCH_PATH, encoding='latin-1') as f:
        lines = f.readlines()

    print(f"  {len(lines)} lines read")

    # Save backup
    print(f"Saving backup: {BACKUP_PATH}")
    shutil.copy2(PATCH_PATH, BACKUP_PATH)

    # Parse and clean
    print("Analyzing hunks...")
    cleaned_lines, stats = parse_and_clean(lines)

    # Write output
    with open(PATCH_PATH, 'w', encoding='latin-1') as f:
        f.writelines(cleaned_lines)

    print(f"Wrote cleaned patch: {PATCH_PATH}")
    print(f"  {len(cleaned_lines)} lines written")
    print()
    print("=" * 60)
    print("STATISTICS")
    print("=" * 60)
    print(f"  Total file sections in patch : {stats['total_files']}")
    print(f"  Total hunks processed        : {stats['total_hunks']}")
    print(f"  Hunks removed (whitespace)   : {stats['removed_ws']}")
    print(f"  Hunks removed (FALLTHROUGH)  : {stats['removed_ft']}")
    print(f"  Hunks removed (total)        : {stats['removed_ws'] + stats['removed_ft']}")
    print(f"  Hunks kept                   : {stats['kept_hunks']}")
    print(f"  File sections emptied+removed: {stats['empty_files']}")
    print()

    reduction = len(lines) - len(cleaned_lines)
    pct = 100.0 * reduction / len(lines) if lines else 0
    print(f"  Lines removed                : {reduction} ({pct:.1f}%)")
    print("=" * 60)


if __name__ == '__main__':
    main()
