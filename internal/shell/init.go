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

// GetInitScript returns the shell initialization script.
func GetInitScript(shell Shell, phvmDir string) string {
	switch shell {
	case Bash:
		return getBashInit(phvmDir)
	case Zsh:
		return getZshInit(phvmDir)
	case Fish:
		return getFishInit(phvmDir)
	case PowerShell:
		return getPowerShellInit(phvmDir)
	default:
		return getBashInit(phvmDir)
	}
}

// DetectShell attempts to detect the current shell.
func DetectShell() Shell {
	if runtime.GOOS == "windows" {
		return PowerShell
	}
	// Default to bash
	return Bash
}

// SupportedShells returns the list of supported shells.
func SupportedShells() []Shell {
	return []Shell{Bash, Zsh, Fish, PowerShell}
}

func getBashInit(phvmDir string) string {
	return fmt.Sprintf(`# phvm initialization for bash
# Add this to your ~/.bashrc or ~/.bash_profile:
# eval "$(phvm init bash)"

export PHVM_DIR="${PHVM_DIR:-%s}"

# Add phvm to PATH
if [[ ":$PATH:" != *":$PHVM_DIR/current/bin:"* ]]; then
    export PATH="$PHVM_DIR/current/bin:$PHVM_DIR/bin:$PATH"
fi

# phvm shell function
phvm() {
    local command="${1:-}"
    
    case "$command" in
        use)
            # Run phvm use and update PATH if needed
            command phvm "$@"
            ;;
        *)
            command phvm "$@"
            ;;
    esac
}

# Bash completion
if command -v phvm &> /dev/null; then
    _phvm_completions() {
        local cur="${COMP_WORDS[COMP_CWORD]}"
        local prev="${COMP_WORDS[COMP_CWORD-1]}"
        
        case "$prev" in
            phvm)
                COMPREPLY=($(compgen -W "install uninstall use ls ls-remote current which alias unalias ini ext composer doctor cache help version" -- "$cur"))
                ;;
            use|uninstall)
                # Complete with installed versions
                local versions=$(phvm ls 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | tr '\n' ' ')
                COMPREPLY=($(compgen -W "$versions" -- "$cur"))
                ;;
            install)
                COMPREPLY=($(compgen -W "8 8.4 8.3 8.2 8.1 latest stable" -- "$cur"))
                ;;
            ini)
                COMPREPLY=($(compgen -W "path open list enable disable profile" -- "$cur"))
                ;;
            ext)
                COMPREPLY=($(compgen -W "list list-remote install uninstall enable disable" -- "$cur"))
                ;;
        esac
    }
    complete -F _phvm_completions phvm
fi
`, phvmDir)
}

func getZshInit(phvmDir string) string {
	return fmt.Sprintf(`# phvm initialization for zsh
# Add this to your ~/.zshrc:
# eval "$(phvm init zsh)"

export PHVM_DIR="${PHVM_DIR:-%s}"

# Add phvm to PATH
if [[ ":$PATH:" != *":$PHVM_DIR/current/bin:"* ]]; then
    export PATH="$PHVM_DIR/current/bin:$PHVM_DIR/bin:$PATH"
fi

# phvm shell function
phvm() {
    local command="${1:-}"
    
    case "$command" in
        use)
            command phvm "$@"
            ;;
        *)
            command phvm "$@"
            ;;
    esac
}

# Zsh completion
if (( $+commands[phvm] )); then
    _phvm() {
        local -a commands
        commands=(
            'install:Install a PHP version'
            'uninstall:Uninstall a PHP version'
            'use:Switch to a PHP version'
            'ls:List installed versions'
            'ls-remote:List available versions'
            'current:Show current version'
            'which:Show path to php binary'
            'alias:Create an alias'
            'unalias:Remove an alias'
            'ini:Manage php.ini'
            'ext:Manage extensions'
            'composer:Manage Composer'
            'doctor:Check system requirements'
            'cache:Manage cache'
            'help:Show help'
            'version:Show version'
        )
        
        if (( CURRENT == 2 )); then
            _describe -t commands 'phvm command' commands
        fi
    }
    compdef _phvm phvm
fi
`, phvmDir)
}

func getFishInit(phvmDir string) string {
	return fmt.Sprintf(`# phvm initialization for fish
# Add this to your ~/.config/fish/config.fish:
# phvm init fish | source

set -gx PHVM_DIR "%s"

# Add phvm to PATH
if not contains "$PHVM_DIR/current/bin" $PATH
    set -gx PATH "$PHVM_DIR/current/bin" "$PHVM_DIR/bin" $PATH
end

# phvm function
function phvm
    switch $argv[1]
        case use
            command phvm $argv
        case '*'
            command phvm $argv
    end
end

# Fish completion
function __fish_phvm_needs_command
    set cmd (commandline -opc)
    if test (count $cmd) -eq 1
        return 0
    end
    return 1
end

function __fish_phvm_using_command
    set cmd (commandline -opc)
    if test (count $cmd) -gt 1
        if test $argv[1] = $cmd[2]
            return 0
        end
    end
    return 1
end

complete -f -c phvm -n __fish_phvm_needs_command -a install -d 'Install a PHP version'
complete -f -c phvm -n __fish_phvm_needs_command -a uninstall -d 'Uninstall a PHP version'
complete -f -c phvm -n __fish_phvm_needs_command -a use -d 'Switch to a PHP version'
complete -f -c phvm -n __fish_phvm_needs_command -a ls -d 'List installed versions'
complete -f -c phvm -n __fish_phvm_needs_command -a ls-remote -d 'List available versions'
complete -f -c phvm -n __fish_phvm_needs_command -a current -d 'Show current version'
complete -f -c phvm -n __fish_phvm_needs_command -a which -d 'Show path to php binary'
complete -f -c phvm -n __fish_phvm_needs_command -a alias -d 'Create an alias'
complete -f -c phvm -n __fish_phvm_needs_command -a unalias -d 'Remove an alias'
complete -f -c phvm -n __fish_phvm_needs_command -a ini -d 'Manage php.ini'
complete -f -c phvm -n __fish_phvm_needs_command -a ext -d 'Manage extensions'
complete -f -c phvm -n __fish_phvm_needs_command -a composer -d 'Manage Composer'
complete -f -c phvm -n __fish_phvm_needs_command -a doctor -d 'Check system requirements'
complete -f -c phvm -n __fish_phvm_needs_command -a cache -d 'Manage cache'
complete -f -c phvm -n __fish_phvm_needs_command -a help -d 'Show help'
complete -f -c phvm -n __fish_phvm_needs_command -a version -d 'Show version'
`, phvmDir)
}

func getPowerShellInit(phvmDir string) string {
	// Convert Unix path to Windows path if needed
	if runtime.GOOS == "windows" {
		phvmDir = strings.ReplaceAll(phvmDir, "/", "\\")
	}

	return fmt.Sprintf(`# phvm initialization for PowerShell
# Add this to your $PROFILE:
# Invoke-Expression (phvm init powershell | Out-String)

$env:PHVM_DIR = if ($env:PHVM_DIR) { $env:PHVM_DIR } else { "%s" }

# Add phvm to PATH
$phvmCurrentBin = Join-Path $env:PHVM_DIR "current\bin"
$phvmBin = Join-Path $env:PHVM_DIR "bin"

if ($env:PATH -notlike "*$phvmCurrentBin*") {
    $env:PATH = "$phvmCurrentBin;$phvmBin;$env:PATH"
}

# phvm function
function global:phvm {
    param(
        [Parameter(Position=0)]
        [string]$Command,
        
        [Parameter(Position=1, ValueFromRemainingArguments=$true)]
        [string[]]$Args
    )
    
    switch ($Command) {
        "use" {
            & (Get-Command -Name phvm -CommandType Application) $Command @Args
        }
        default {
            & (Get-Command -Name phvm -CommandType Application) $Command @Args
        }
    }
}

# Tab completion
Register-ArgumentCompleter -Native -CommandName phvm -ScriptBlock {
    param($wordToComplete, $commandAst, $cursorPosition)
    
    $commands = @(
        [CompletionResult]::new('install', 'install', 'ParameterValue', 'Install a PHP version')
        [CompletionResult]::new('uninstall', 'uninstall', 'ParameterValue', 'Uninstall a PHP version')
        [CompletionResult]::new('use', 'use', 'ParameterValue', 'Switch to a PHP version')
        [CompletionResult]::new('ls', 'ls', 'ParameterValue', 'List installed versions')
        [CompletionResult]::new('ls-remote', 'ls-remote', 'ParameterValue', 'List available versions')
        [CompletionResult]::new('current', 'current', 'ParameterValue', 'Show current version')
        [CompletionResult]::new('which', 'which', 'ParameterValue', 'Show path to php binary')
        [CompletionResult]::new('alias', 'alias', 'ParameterValue', 'Create an alias')
        [CompletionResult]::new('unalias', 'unalias', 'ParameterValue', 'Remove an alias')
        [CompletionResult]::new('ini', 'ini', 'ParameterValue', 'Manage php.ini')
        [CompletionResult]::new('ext', 'ext', 'ParameterValue', 'Manage extensions')
        [CompletionResult]::new('composer', 'composer', 'ParameterValue', 'Manage Composer')
        [CompletionResult]::new('doctor', 'doctor', 'ParameterValue', 'Check system requirements')
        [CompletionResult]::new('cache', 'cache', 'ParameterValue', 'Manage cache')
        [CompletionResult]::new('help', 'help', 'ParameterValue', 'Show help')
        [CompletionResult]::new('version', 'version', 'ParameterValue', 'Show version')
    )
    
    $commands | Where-Object { $_.CompletionText -like "$wordToComplete*" }
}
`, phvmDir)
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
