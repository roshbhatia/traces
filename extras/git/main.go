// traces-provider-git renders a two-file diff through Git.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type validationCheck struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

func main() {
	validate := flag.Bool("validate", false, "check provider dependencies")
	flag.Parse()
	if *validate {
		writeValidation()
		return
	}
	if flag.NArg() != 4 {
		fmt.Fprintln(os.Stderr, "usage: traces-provider-git LOCAL REMOTE MERGED COLOR")
		os.Exit(2)
	}
	output, status, err := renderDiff(flag.Arg(0), flag.Arg(1), flag.Arg(2), flag.Arg(3))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(status)
	}
	fmt.Print(output)
	os.Exit(status)
}

func writeValidation() {
	path, err := exec.LookPath("git")
	check := validationCheck{Kind: "command", Name: "git", Status: "ok", Message: path}
	if err != nil {
		check.Status = "failed"
		check.Message = err.Error()
	}
	report := struct {
		Checks []validationCheck `json:"checks"`
	}{Checks: []validationCheck{check}}
	if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func renderDiff(local, remote, merged, color string) (string, int, error) {
	command := exec.Command("git", "-c", "core.quotePath=false", "diff", "--no-index", "--color="+color, "--", local, remote)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	status := 0
	if err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok {
			return "", 1, err
		}
		status = exit.ExitCode()
		if status != 1 {
			message := strings.TrimSpace(stderr.String())
			if message == "" {
				message = err.Error()
			}
			return "", status, fmt.Errorf("git diff: %s", message)
		}
	}
	return replaceLabels(string(output), local, remote, merged), status, nil
}

func replaceLabels(output, local, remote, merged string) string {
	localLabel := strings.TrimPrefix(filepath.ToSlash(local), "/")
	remoteLabel := strings.TrimPrefix(filepath.ToSlash(remote), "/")
	mergedLabel := strings.TrimPrefix(filepath.ToSlash(strings.TrimPrefix(merged, "./")), "/")
	output = strings.ReplaceAll(output, "a/"+localLabel, "a/"+mergedLabel)
	return strings.ReplaceAll(output, "b/"+remoteLabel, "b/"+mergedLabel)
}
