# Development Guide

## macOS Setup

The build system uses scripts from the `hack/common` submodule that rely on GNU coreutils (specifically GNU `realpath` with the `--relative-base` option). On macOS, BSD `realpath` is installed by default and doesn't support these options.

### Solution

Install GNU coreutils via Homebrew:

```bash
brew install coreutils
```

Then either:

**Option 1: Export PATH before running tasks (recommended)**

```bash
export PATH="$PWD/bin:$PATH"
task test-e2e
```

Or add to your shell profile (`.zshrc`, `.bashrc`):

```bash
export PATH="/Users/your-username/path/to/service-provider-odg/bin:$PATH"
```

**Option 2: Create a system-wide symlink**

```bash
sudo ln -s $(brew --prefix coreutils)/bin/grealpath /usr/local/bin/realpath
```

### How it works

The `bin/realpath` script is a wrapper that:
- Detects if GNU `grealpath` is available (installed via coreutils)
- Falls back to system `realpath` if not available
- Allows build scripts to use GNU realpath features transparently

This wrapper is checked into git to help all macOS developers, but requires `PATH="$PWD/bin:$PATH"` to be effective since Task spawns subshells.

### CI

CI runs on Linux where GNU `realpath` is the default, so no special setup is needed there.
