# alternatives experiment source

Historical standalone module for the 2026-10-15 experiments. This code predates
the public pwr API and is not imported by it. See [results and methodology](../../../docs/experiments/2026-10-15/alternatives/REPORT.ko.md).

Run `sh reproduce.sh` here. Go 1.27.1 was used. The alternatives module also
needs a C compiler and Rust 1.99.0 to build all variants. Dependencies and the
public cr source revision are pinned in go.mod. The measured binaries used a
local checkout of the same cr commit; path/module-metadata changes can alter
rebuilt hashes and sizes.
