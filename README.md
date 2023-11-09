Gomass
======

Summary
-------

Figure out what's giving your Go binaries so much mass.  This provides a report
broken out by package and symbol so that you can see exactly where your
precious storage is being used.

Installation
------------

This project can be directly installed via `go`:

```shell
go install github.com/andrewbenton/gomass@latest
```

Usage
-----

View the help:

```bash
gomass -h

# Usage of gomass:
#  -b, --binary string
#  -f, --format string   Select the output format from [json] (default "json")
#  -s, --skip-symbols    Skip emitting granular symbol data
# pflag: help requested
```

Emit JSON for analysis:

```bash
gomass -b <my_binary>
```

Show a UI:

```bash
gomass -b <my_binary> -f ui
```

![tree_view](https://github.com/andrewbenton/gomass/raw/main/img/tree_view.png)

Hide symbols from the report:

```bash
gomass -b <my_binary> -s
```

Notes
-----

This analyzes binary space based on the symbol table.  This will not be
perfectly accurate and should only be used as a quick, general guide.  If your
binary has had the symbol table stripped, this will not be able to run.
