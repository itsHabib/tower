package main

import (
	"errors"
	"flag"
	"fmt"
)

const bashHelper = `# tower shell helper — add ` + "`tcd <name>`" + ` to jump into a worktree.
# Wire up with:  eval "$(tower shell bash)"
tcd() {
    local path
    path=$(tower open "$@") || return $?
    cd "$path" || return $?
}
`

const powershellHelper = `# tower shell helper — adds Tcd <name> to jump into a worktree.
# Wire up by adding to your $PROFILE:  Invoke-Expression (& tower shell powershell | Out-String)
function Tcd {
    param([Parameter(Mandatory)][string]$Name)
    $path = & tower open $Name
    if ($LASTEXITCODE -ne 0) { return }
    Set-Location $path
}
`

func cmdShell(args []string) error {
	fs := flag.NewFlagSet("shell", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	shell := "bash"
	if fs.NArg() > 0 {
		shell = fs.Arg(0)
	}
	switch shell {
	case "bash", "zsh", "sh":
		fmt.Print(bashHelper)
	case "powershell", "pwsh":
		fmt.Print(powershellHelper)
	default:
		return errors.New("supported shells: bash, zsh, sh, powershell")
	}
	return nil
}
