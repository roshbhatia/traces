package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func main() {
	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "traces: resolve full launcher: %v\n", err)
		os.Exit(1)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		fmt.Fprintf(os.Stderr, "traces: resolve full launcher symlinks: %v\n", err)
		os.Exit(1)
	}
	directory := filepath.Dir(executable)
	prependEnvironment("PATH", directory)
	prependEnvironment("XDG_DATA_DIRS", filepath.Join(directory, "share"))
	core := filepath.Join(directory, ".traces-core")
	if err := syscall.Exec(core, append([]string{"traces"}, os.Args[1:]...), os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "traces: start bundled core: %v\n", err)
		os.Exit(1)
	}
}

func prependEnvironment(name, value string) {
	if existing := os.Getenv(name); existing != "" {
		value += string(os.PathListSeparator) + existing
	}
	_ = os.Setenv(name, value)
}
