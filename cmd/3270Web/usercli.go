package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"golang.org/x/term"

	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/users"
)

const userUsage = `Manage local 3270Web accounts.

Usage:
  3270Web user add <username> [--admin]   Create an account
  3270Web user list                       List accounts
  3270Web user passwd <username>          Set an account's password
  3270Web user enable <username>          Re-enable a disabled account
  3270Web user disable <username>         Disable an account

Passwords are read from the terminal, or from stdin when it is not a
terminal. They are never taken from the command line, where they would be
visible in the process list and the shell history.
`

// runUserCLI implements the `user` subcommand.
//
// It writes to stdout/stderr and returns an exit code rather than calling
// os.Exit, so the whole thing stays testable.
func runUserCLI(args []string, stdout, stderr io.Writer, stdin io.Reader) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, userUsage)
		return 2
	}

	store := users.NewStore(resolveUsersPath())

	switch args[0] {
	case "add":
		return userAdd(store, args[1:], stdout, stderr, stdin)
	case "list":
		return userList(store, stdout, stderr)
	case "passwd":
		return userPasswd(store, args[1:], stdout, stderr, stdin)
	case "enable":
		return userSetDisabled(store, args[1:], false, stdout, stderr)
	case "disable":
		return userSetDisabled(store, args[1:], true, stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, userUsage)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown user command %q\n\n%s", args[0], userUsage)
		return 2
	}
}

// resolveUsersPath mirrors where newApp puts the store, so the CLI and the
// server always agree on which file they are talking about.
func resolveUsersPath() string {
	if override := strings.TrimSpace(os.Getenv("USERS_PATH")); override != "" {
		return override
	}
	return filepath.Join(resolveBaseDir(), "users.json")
}

func userAdd(store *users.Store, args []string, stdout, stderr io.Writer, stdin io.Reader) int {
	var username string
	role := authz.RoleUser
	for _, arg := range args {
		switch arg {
		case "--admin":
			role = authz.RoleAdmin
		case "--user":
			role = authz.RoleUser
		default:
			if strings.HasPrefix(arg, "-") {
				fmt.Fprintf(stderr, "unknown option %q\n", arg)
				return 2
			}
			if username != "" {
				fmt.Fprintf(stderr, "unexpected argument %q\n", arg)
				return 2
			}
			username = arg
		}
	}
	if username == "" {
		fmt.Fprint(stderr, "a username is required\n\n"+userUsage)
		return 2
	}
	if err := users.ValidateUsername(username); err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}

	password, err := readNewPassword(stdout, stdin)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}

	user, err := store.Add(username, password, role, false)
	if err != nil {
		if errors.Is(err, users.ErrUserExists) {
			fmt.Fprintf(stderr, "a user named %q already exists\n", username)
			return 1
		}
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "created %s (%s)\n", user.Username, user.Role)
	return 0
}

func userList(store *users.Store, stdout, stderr io.Writer) int {
	list, err := store.List()
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	if len(list) == 0 {
		fmt.Fprintln(stdout, "no accounts yet")
		return 0
	}

	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "USERNAME\tROLE\tSTATUS\tCREATED")
	for _, u := range list {
		status := "enabled"
		if u.Disabled {
			status = "disabled"
		} else if u.MustChangePassword {
			status = "must change password"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", u.Username, u.Role, status, u.CreatedAt.Format("2006-01-02"))
	}
	w.Flush()
	return 0
}

func userPasswd(store *users.Store, args []string, stdout, stderr io.Writer, stdin io.Reader) int {
	if len(args) != 1 {
		fmt.Fprint(stderr, "a username is required\n\n"+userUsage)
		return 2
	}
	password, err := readNewPassword(stdout, stdin)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	if err := store.SetPassword(args[0], password); err != nil {
		if errors.Is(err, users.ErrUserNotFound) {
			fmt.Fprintf(stderr, "no user named %q\n", args[0])
			return 1
		}
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	// The server holds logins in memory, so a running instance keeps its
	// existing sessions until it notices. Say so rather than implying the
	// change took effect everywhere.
	fmt.Fprintf(stdout, "password updated for %s\n", args[0])
	fmt.Fprintln(stdout, "note: sessions already signed in on a running server stay signed in until it restarts")
	return 0
}

func userSetDisabled(store *users.Store, args []string, disabled bool, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprint(stderr, "a username is required\n\n"+userUsage)
		return 2
	}
	if err := store.SetDisabled(args[0], disabled); err != nil {
		if errors.Is(err, users.ErrUserNotFound) {
			fmt.Fprintf(stderr, "no user named %q\n", args[0])
			return 1
		}
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	if disabled {
		fmt.Fprintf(stdout, "disabled %s\n", args[0])
	} else {
		fmt.Fprintf(stdout, "enabled %s\n", args[0])
	}
	return 0
}

// readNewPassword prompts twice on a terminal, or reads one line from stdin
// when it is piped.
//
// The password never comes from argv: arguments are visible to every other
// process on the machine and land in shell history.
func readNewPassword(stdout io.Writer, stdin io.Reader) (string, error) {
	if f, ok := stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprint(stdout, "New password: ")
		first, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(stdout)
		if err != nil {
			return "", fmt.Errorf("read password: %w", err)
		}
		fmt.Fprint(stdout, "Confirm password: ")
		second, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(stdout)
		if err != nil {
			return "", fmt.Errorf("read password: %w", err)
		}
		if string(first) != string(second) {
			return "", errors.New("the passwords do not match")
		}
		return validated(string(first))
	}

	reader := bufio.NewReader(stdin)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return "", errors.New("no password on stdin")
	}
	return validated(strings.TrimRight(line, "\r\n"))
}

func validated(password string) (string, error) {
	if err := users.ValidatePassword(password); err != nil {
		return "", err
	}
	return password, nil
}
