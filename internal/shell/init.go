// Package shell provides shell initialization scripts.
package shell

import (
	"fmt"
	"runtime"
	"strings"
)

// Shell represents a shell type.
type Shell string

const (
	Bash       Shell = "bash"
	Zsh        Shell = "zsh"
	Fish       Shell = "fish"
	PowerShell Shell = "powershell"
)

// GetInitScript returns an initialization script that keeps an existing PHVM_DIR.
func GetInitScript(sh Shell, root string) string {
	return GetInitScriptWithOverride(sh, root, false)
}

// GetInitScriptWithOverride lets an explicit CLI root replace an existing PHVM_DIR.
func GetInitScriptWithOverride(sh Shell, root string, override bool) string {
	switch sh {
	case Bash, Zsh:
		return posixInit(sh, root, override)
	case Fish:
		return fishInit(root, override)
	case PowerShell:
		return powerShellInit(root, override)
	default:
		return posixInit(Bash, root, override)
	}
}

// DetectShell attempts to detect the current shell.
func DetectShell() Shell {
	if runtime.GOOS == "windows" {
		return PowerShell
	}
	return Bash
}

// SupportedShells returns the list of supported shells.
func SupportedShells() []Shell {
	return []Shell{Bash, Zsh, Fish, PowerShell}
}

func quotePOSIX(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func quoteFish(value string) string {
	escaped := strings.ReplaceAll(value, "\\", "\\\\")
	return "'" + strings.ReplaceAll(escaped, "'", "\\'") + "'"
}

func quotePowerShell(value string) string {
	quotes := strings.NewReplacer("'", "''", "‘", "‘‘", "’", "’’")
	return "'" + quotes.Replace(value) + "'"
}

func posixInit(sh Shell, root string, override bool) string {
	assignment := "if [[ -z ${PHVM_DIR:-} ]]; then\n    export PHVM_DIR=" + quotePOSIX(root) + "\nfi"
	if override {
		assignment = "export PHVM_DIR=" + quotePOSIX(root)
	}
	completionSetup := ""
	if sh == Zsh {
		completionSetup = `# Cobra's generated zsh completion needs compdef.
if ! (( $+functions[compdef] )); then
    autoload -Uz compinit
    compinit -D
fi
`
	}
	return fmt.Sprintf(`# phvm initialization for %s
%s

# Add both managed directories exactly once, preserving existing PATH entries.
case ":$PATH:" in
    *":$PHVM_DIR/bin:"*) ;;
    *) export PATH="$PHVM_DIR/bin${PATH:+:$PATH}" ;;
esac
case ":$PATH:" in
    *":$PHVM_DIR/current/bin:"*) ;;
    *) export PATH="$PHVM_DIR/current/bin${PATH:+:$PATH}" ;;
esac
%s`, sh, assignment, completionSetup)
}

func fishInit(root string, override bool) string {
	assignment := "if not set -q PHVM_DIR; or test -z \"$PHVM_DIR\"\n    set -gx PHVM_DIR " + quoteFish(root) + "\nend"
	if override {
		assignment = "set -gx PHVM_DIR " + quoteFish(root)
	}
	return fmt.Sprintf(`# phvm initialization for fish
%s

if not contains -- "$PHVM_DIR/bin" $PATH
    set -gx PATH "$PHVM_DIR/bin" $PATH
end
if not contains -- "$PHVM_DIR/current/bin" $PATH
    set -gx PATH "$PHVM_DIR/current/bin" $PATH
end
`, assignment)
}

func powerShellInit(root string, override bool) string {
	if runtime.GOOS == "windows" {
		root = strings.ReplaceAll(root, "/", "\\")
	}
	assignment := "if ([string]::IsNullOrEmpty($env:PHVM_DIR)) { $env:PHVM_DIR = " + quotePowerShell(root) + " }"
	if override {
		assignment = "$env:PHVM_DIR = " + quotePowerShell(root)
	}
	return fmt.Sprintf(`# phvm initialization for PowerShell
%s

$phvmPathSeparator = [string][IO.Path]::PathSeparator
$phvmPathEntries = $env:PATH -split [regex]::Escape($phvmPathSeparator)
$phvmBin = Join-Path $env:PHVM_DIR 'bin'
$phvmCurrentBin = Join-Path $env:PHVM_DIR 'current/bin'
if ($phvmPathEntries -notcontains $phvmBin) {
    $env:PATH = "$phvmBin$phvmPathSeparator$env:PATH"
}
if ($phvmPathEntries -notcontains $phvmCurrentBin) {
    $env:PATH = "$phvmCurrentBin$phvmPathSeparator$env:PATH"
}
`, assignment)
}

// GetProfileInstructions returns instructions for adding to shell profile.
func GetProfileInstructions(shell Shell) string {
	switch shell {
	case Bash:
		return `Add the following to your ~/.bashrc or ~/.bash_profile:

    eval "$(phvm init bash)"

Then restart your shell or run:

    source ~/.bashrc`
	case Zsh:
		return `Add the following to your ~/.zshrc:

    eval "$(phvm init zsh)"

Then restart your shell or run:

    source ~/.zshrc`
	case Fish:
		return `Add the following to your ~/.config/fish/config.fish:

    phvm init fish | source

Then restart your shell or run:

    source ~/.config/fish/config.fish`
	case PowerShell:
		return `Add the following to your PowerShell profile ($PROFILE):

    Invoke-Expression (phvm init powershell | Out-String)

Then restart PowerShell or run:

    . $PROFILE`
	default:
		return ""
	}
}
