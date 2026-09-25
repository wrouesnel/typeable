# typeable

Utility to create and receive bodies of text which may be typed into terminals
by humans.

## How It Works

`typeable encode` reads plaintext data from `stdin`, compresses it, and base32
encodes the result. The base32 encoding is then split along 5 character boundaries,
and collected as groups of 7. Reed-Soloman encoding creates an 8th group to validate
and repair mistypes on individual lines.

Grid columns are lettered, and grid rows are numbered for ease of typing.

Example structure:

```
typeable encode < somefile

16      A       B       C       D        E       F       G         H

1    <data1> <data2> <data3> <data4>  <data5> <data6> <data7> <parity block>
2    <data1> <data2> <data3> <data4>  <data5> <data6> <data7> <parity block>
```

The above grid shows top-left, the number of data blocks which are encoded, then
the column numbering (to aid retyping), the row numbering (to aid retyping) and
the groups of 5 characters 7 per row, with an 8th parity block.

## Usage

```sh
typeable encode < somefile > grid.txt
typeable decode > somefile
```

Blocks are printed in lowercase.

When stdin is a terminal, `typeable decode` opens an interactive editor. It asks
for the number of data blocks (the number at the top left of the grid), then shows
an empty grid to fill in:

* Type each row. A full block moves on to the next one, so there's no need to type
  spaces.
* Arrow keys move between characters, blocks and rows. Tab/Shift-Tab and Space
  jump between blocks, and Enter jumps to the next unfinished row.
* Each row is checked once it's full, or once you leave it partly typed (blank
  characters are treated as unknown and filled in by the parity if possible).
  Repaired characters are highlighted, and the focused row lists what was fixed.
  Rows with too many mistakes are marked `✗ retype`.
* When every row is valid, press Enter to write the decoded data to stdout. Esc
  or Ctrl-C quits without decoding.

The editor draws on stderr, so `typeable decode > somefile` works as expected.

When stdin isn't a terminal, or with `--lines`, the grid is read line by line: the
header line first, then rows in any order. Repairs are reported on stderr, a row
with too many mistakes is rejected (enter it again to replace it), and decoding
finishes once every row has been entered.

Exit codes:

| Code | Meaning |
|---|---|
| 0 | Success. |
| 1 | Failure, such as missing rows or a checksum mismatch. The reason is printed on stderr. |
| 130 | Decoding was cancelled (Esc or Ctrl-C). Nothing is printed. |

Entry is forgiving:

* Case doesn't matter (in either mode), and `O` is read as `0` and `I`/`L` as `1`.
* In line mode, spacing and hyphens are ignored when a row has the right number of
  characters. If it doesn't, blocks are matched up by spaces, and a block of the wrong length
  (a dropped or doubled character) is treated as unknown and filled in by the parity.

## Format details

* **Encoding:** the payload is a CRC-32 of the plaintext followed by a raw deflate
  stream, written in [Crockford's base32](https://www.crockford.com/base32.html)
  (`0-9` and `A-Z` without `I`, `L`, `O` or `U`). The last block is padded with `0`.
* **Parity:** each row is protected by Reed-Solomon over GF(32), one symbol per
  character. A GF(32) codeword holds at most 31 characters, so each row is split
  into two codewords:
  * The 1st, 3rd and 5th characters of every block, with 3 parity characters.
  * The 2nd and 4th characters of every block, with 2 parity characters.

  This means that per row, either of these can be repaired:
  * any one wrong character, or two swapped characters inside a block;
  * one whole block that is unknown.
* **Checksum:** the CRC-32 catches the rare mistakes the parity miscorrects.
* **Short last row:** it holds only the remaining data blocks, with its parity
  block still under column `H`.

## Structure

| Path | Purpose |
|---|---|
| `cmd/typeable/main.go` | Minimal `main` package. It only calls the entrypoint. |
| `pkg/entrypoints/typeable/` | Command line parsing, exit codes and the `encode`/`decode` commands. |
| `pkg/codec/` | The grid format: payload framing, row layout, formatting and the line-by-line decoder. |
| `pkg/rs32/` | Reed-Solomon encoding and error/erasure decoding over GF(32). |
| `pkg/decodetui/` | The interactive decode editor, built with [Bubble Tea](https://github.com/charmbracelet/bubbletea). |
| `pkg/crockford/` | Crockford base32 symbols. |
| `version/` | Application name, description and version. `Version` is set at link time by the build. |
| `magefile.go`, `mage.go` | The build system. Run `go run mage.go -l` to list targets. |
| `.github/workflows/` | CI: build and test on every push, releases on `v*` tags. |

## Building

```sh
go run mage.go binary
./typeable --help
```

`go run mage.go binary` builds into `bin/` and symlinks the binary into the repository root.
`go run mage.go releaseAll` cross-compiles release archives for every platform into
`release/`.
