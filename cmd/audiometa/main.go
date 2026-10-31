// Command audiometa prints audio file metadata.
//
// Run "audiometa help" for the list of subcommands and the exit codes.
package main

import "os"

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
